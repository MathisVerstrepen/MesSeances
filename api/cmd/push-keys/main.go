// Command push-keys prints a new VAPID key pair as deploy/.env lines. The
// private key is a secret: store it only in the deployment environment.
package main

import (
	"fmt"
	"os"

	"messeances/api/internal/webpush"
)

func main() {
	public, private, err := webpush.GenerateKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "key generation failed")
		os.Exit(1)
	}
	fmt.Printf("WEB_PUSH_VAPID_PUBLIC_KEY=%s\nWEB_PUSH_VAPID_PRIVATE_KEY=%s\n", public, private)
}
