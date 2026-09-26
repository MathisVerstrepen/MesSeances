package accountmail

import "context"

// Message exists in memory during rendering/delivery only. Persist an encrypted
// envelope, never this plaintext or a provider response. Templates escape HTML.
type Message struct {
	Recipient string
	Subject   string
	Text      string
	HTML      string
}

// Sender is injected into the outbox worker. Nil is unavailable, never a silent
// success. A successful send means provider acceptance, not mailbox delivery.
type Sender interface {
	Send(context.Context, Message) error
}

// PayloadCipher permits test encryption without network or production keys.
type PayloadCipher interface {
	Seal(binding string, plaintext []byte) (Envelope, error)
	Open(binding string, envelope Envelope) ([]byte, error)
}
