// Command sign signs release binaries with the key in .private/update-signing.key.
//
//	go run ./scripts/sign keygen              makes the key once and prints its public half
//	go run ./scripts/sign check               the key is here and is the one the program trusts
//	go run ./scripts/sign 1.2.3 amd64 FILE    prints the signature
//	go run ./scripts/sign 1.2.3 android/arm64-v8a FILE   the same for the television app
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"trackferry/internal/update"
)

const keyFile = ".private/update-signing.key"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func load() (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New(keyFile + " 不是签名私钥")
	}
	key := ed25519.NewKeyFromSeed(seed)
	if !update.KeyMatches(key.Public().(ed25519.PublicKey)) {
		return nil, errors.New(keyFile + " 和程序里的公钥不是一对")
	}
	return key, nil
}

func run(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "keygen":
		pub, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(keyFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(key.Seed())); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return f.Close()
	case len(args) == 1 && args[0] == "check":
		_, err := load()
		return err
	case len(args) == 3:
		key, err := load()
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(key, update.Message(args[0], args[1], hex.EncodeToString(sum[:])))))
		return nil
	}
	return errors.New("用法：keygen | check | 版本 架构 文件")
}
