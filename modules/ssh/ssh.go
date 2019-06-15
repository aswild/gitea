// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2017 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Unknwon/com"
	"golang.org/x/crypto/ssh"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/setting"
)

// Parse a string out of an SSH payload.
// SSH strings are encoded as a uint32 length (network byte order) followed by the data
// See RFC 4251 section 5
// Returns the string value and the rest of the payload
func parseSshString(payload []byte) (string, []byte, error) {
	if len(payload) < 4 {
		return "", payload, fmt.Errorf("invalid SSH payload length (no size)")
	}
	length := binary.BigEndian.Uint32(payload[:4])
	if uint32(len(payload)) < 4+length {
		return "", payload, fmt.Errorf("invalid SSH payload length (missing data)")
	}

	s := string(payload[4 : 4+length])
	rest := payload[4+length:]
	return s, rest, nil
}

func cleanCommand(payload []byte) (string, error) {
	// exec payload is a single string (RFC 4254 section 6.5)
	cmd, _, err := parseSshString(payload)
	if err != nil {
		return cmd, err
	}

	i := strings.Index(cmd, "git")
	if i == -1 {
		return cmd, fmt.Errorf("only git commands are supported")
	}
	cmd = strings.TrimLeft(cmd[i:], "'()")
	return cmd, nil
}

func handleServerConn(keyID string, chans <-chan ssh.NewChannel) {
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			err := newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			if err != nil {
				log.Error("Error rejecting channel: %v", err)
			}
			continue
		}

		ch, reqs, err := newChan.Accept()
		if err != nil {
			log.Error("Error accepting channel: %v", err)
			continue
		}

		go func(in <-chan *ssh.Request) {
			defer func() {
				if err = ch.Close(); err != nil {
					log.Error("Close: %v", err)
				}
			}()
			for req := range in {
				switch req.Type {
				case "exec":
					cmdName, err := cleanCommand(req.Payload)
					log.Trace("SSH: Payload: %q", cmdName)

					if err != nil {
						req.Reply(true, nil)
						fmt.Fprintf(ch, "Gitea: invalid command: %q: %v\n", cmdName, err)
						ch.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
						return
					}

					args := []string{"serv", "key-" + keyID, "--config=" + setting.CustomConf}
					log.Trace("SSH: Arguments: %v", args)
					cmd := exec.Command(setting.AppPath, args...)
					cmd.Env = append(
						os.Environ(),
						"SSH_ORIGINAL_COMMAND="+cmdName,
						"SKIP_MINWINSVC=1",
					)

					stdout, err := cmd.StdoutPipe()
					if err != nil {
						log.Error("SSH: StdoutPipe: %v", err)
						return
					}
					stderr, err := cmd.StderrPipe()
					if err != nil {
						log.Error("SSH: StderrPipe: %v", err)
						return
					}
					input, err := cmd.StdinPipe()
					if err != nil {
						log.Error("SSH: StdinPipe: %v", err)
						return
					}

					// FIXME: check timeout
					if err = cmd.Start(); err != nil {
						log.Error("SSH: Start: %v", err)
						return
					}

					err = req.Reply(true, nil)
					if err != nil {
						log.Error("SSH: Reply: %v", err)
					}
					go func() {
						_, err = io.Copy(input, ch)
						if err != nil {
							log.Error("SSH: Copy: %v", err)
						}
					}()
					_, err = io.Copy(ch, stdout)
					if err != nil {
						log.Error("SSH: Copy: %v", err)
					}
					_, err = io.Copy(ch.Stderr(), stderr)
					if err != nil {
						log.Error("SSH: Copy: %v", err)
					}

					if err = cmd.Wait(); err != nil {
						log.Error("SSH: Wait: %v", err)
						return
					}

					_, err = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					if err != nil {
						log.Error("SSH: SendRequest: %v", err)
					}
					return
				case "shell":
					req.Reply(true, nil)
					io.WriteString(ch, "Hi there, You've successfully authenticated, but Gitea does not provide shell access.\n")
					io.WriteString(ch, "If this is unexpected, please log in with password and setup Gitea under another user.\n")
					ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return
				default:
					// reject all other request types (e.g. pty-req)
					log.Trace("SSH: Rejecting request type %v", req.Type)
					req.Reply(false, nil)
				}
			}
		}(reqs)
	}
}

func listen(config *ssh.ServerConfig, host string, port int) {
	listener, err := net.Listen("tcp", host+":"+com.ToStr(port))
	if err != nil {
		log.Fatal("Failed to start SSH server: %v", err)
	}
	for {
		// Once a ServerConfig has been configured, connections can be accepted.
		conn, err := listener.Accept()
		if err != nil {
			log.Error("SSH: Error accepting incoming connection: %v", err)
			continue
		}

		// Before use, a handshake must be performed on the incoming net.Conn.
		// It must be handled in a separate goroutine,
		// otherwise one user could easily block entire loop.
		// For example, user could be asked to trust server key fingerprint and hangs.
		go func() {
			log.Trace("SSH: Handshaking for %s", conn.RemoteAddr())
			sConn, chans, reqs, err := ssh.NewServerConn(conn, config)
			if err != nil {
				if err == io.EOF {
					log.Warn("SSH: Handshaking with %s was terminated: %v", conn.RemoteAddr(), err)
				} else {
					log.Error("SSH: Error on handshaking with %s: %v", conn.RemoteAddr(), err)
				}
				return
			}

			log.Trace("SSH: Connection from %s (%s)", sConn.RemoteAddr(), sConn.ClientVersion())
			// The incoming Request channel must be serviced.
			go ssh.DiscardRequests(reqs)
			go handleServerConn(sConn.Permissions.Extensions["key-id"], chans)
		}()
	}
}

// Listen starts a SSH server listens on given port.
func Listen(host string, port int, ciphers []string, keyExchanges []string, macs []string) {
	config := &ssh.ServerConfig{
		Config: ssh.Config{
			Ciphers:      ciphers,
			KeyExchanges: keyExchanges,
			MACs:         macs,
		},
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			user := conn.User()
			if user != setting.SSH.BuiltinServerUser {
				return nil, fmt.Errorf("username %q doesn't match configured built-in SSH username %q",
					user, setting.SSH.BuiltinServerUser)
			}
			pkey, err := models.SearchPublicKeyByContent(strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))))
			if err != nil {
				log.Error("SearchPublicKeyByContent: %v", err)
				return nil, err
			}
			return &ssh.Permissions{Extensions: map[string]string{"key-id": com.ToStr(pkey.ID)}}, nil
		},
	}

	// look for all supported ssh_host_*_key formats
	keyFiles := make([]string, 0, 1)
	for _, keyType := range [...]string{"rsa", "dsa", "ecdsa", "ed25519"} {
		keyPath := filepath.Join(setting.AppDataPath, "ssh/ssh_host_"+keyType+"_key")
		if com.IsExist(keyPath) {
			keyFiles = append(keyFiles, keyPath)
		}
	}

	// also check for legacy gogs.rsa, only if no openssh-named keys were found
	oldKeyFile := filepath.Join(setting.AppDataPath, "ssh/gogs.rsa")
	if len(keyFiles) == 0 && com.IsExist(oldKeyFile) {
		keyFiles = append(keyFiles, oldKeyFile)
	}

	// if no keys found, create an RSA key
	if len(keyFiles) == 0 {
		keyPath := filepath.Join(setting.AppDataPath, "ssh/ssh_host_rsa_key")
		filePath := filepath.Dir(keyPath)

		if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
			log.Error("Failed to create dir %s: %v", filePath, err)
		}

		err := GenKeyPair(keyPath)
		if err != nil {
			log.Fatal("Failed to generate private key: %v", err)
		}
		log.Trace("SSH: New private key is generateed: %s", keyPath)
		keyFiles = append(keyFiles, keyPath)
	}

	for _, keyPath := range keyFiles {
		privateBytes, err := ioutil.ReadFile(keyPath)
		if err != nil {
			log.Fatal("SSH: Failed to load private key %s: %v", keyPath, err)
		}
		private, err := ssh.ParsePrivateKey(privateBytes)
		if err != nil {
			log.Fatal("SSH: Failed to parse private key %s: %v", keyPath, err)
		}
		config.AddHostKey(private)
		log.Trace("SSH: loaded host key %s", keyPath)
	}

	go listen(config, host, port)
}

// GenKeyPair make a pair of public and private keys for SSH access.
// Public key is encoded in the format for inclusion in an OpenSSH authorized_keys file.
// Private Key generated is PEM encoded
func GenKeyPair(keyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	f, err := os.OpenFile(keyPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err = f.Close(); err != nil {
			log.Error("Close: %v", err)
		}
	}()

	if err := pem.Encode(f, privateKeyPEM); err != nil {
		return err
	}

	// generate public key
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return err
	}

	public := ssh.MarshalAuthorizedKey(pub)
	p, err := os.OpenFile(keyPath+".pub", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err = p.Close(); err != nil {
			log.Error("Close: %v", err)
		}
	}()
	_, err = p.Write(public)
	return err
}
