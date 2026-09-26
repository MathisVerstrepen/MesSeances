package accountmail

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type fakeSES struct {
	input    *sesv2.SendEmailInput
	attempts int
	err      error
}

func (s *fakeSES) SendEmail(_ context.Context, in *sesv2.SendEmailInput, opts ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	s.input = in
	var o sesv2.Options
	for _, apply := range opts {
		apply(&o)
	}
	s.attempts = o.RetryMaxAttempts
	return &sesv2.SendEmailOutput{}, s.err
}
func TestSESSingleAttemptAndSafeErrors(t *testing.T) {
	client := &fakeSES{}
	sender := &SESSender{Client: client, From: "no-reply@messeances.fr", FromName: "MesSeances", IdentityARN: "identity", ConfigurationSet: "accounts"}
	message := Message{Recipient: "recipient@example.com", Subject: "subject", Text: "plain", HTML: "<p>escaped</p>"}
	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if client.attempts != 1 || aws.ToString(client.input.ConfigurationSetName) != "accounts" || aws.ToString(client.input.FromEmailAddressIdentityArn) != "identity" || len(client.input.Destination.ToAddresses) != 1 || aws.ToString(client.input.Content.Simple.Body.Html.Data) != message.HTML {
		t.Fatal("invalid SES request")
	}
	if got := aws.ToString(client.input.FromEmailAddress); got != `"MesSeances" <no-reply@messeances.fr>` {
		t.Fatalf("FromEmailAddress=%q", got)
	}
	for _, tc := range []struct{ err, want error }{
		{context.DeadlineExceeded, ErrTransient}, {io.ErrUnexpectedEOF, ErrTransient},
		{&smithy.GenericAPIError{Code: "TooManyRequestsException", Message: "secret"}, ErrTransient},
		{&smithy.GenericAPIError{Code: "MessageRejected", Message: "secret"}, ErrRejected},
		{&smithy.GenericAPIError{Code: "AccessDeniedException", Message: "secret"}, ErrRejected},
		{&smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 503}}, Err: errors.New("secret")}, ErrTransient},
		{errors.New("secret"), ErrRejected},
	} {
		client.err = tc.err
		if got := sender.Send(context.Background(), message); !errors.Is(got, tc.want) {
			t.Fatalf("classification: %v", got)
		}
	}
}

func TestSESSenderFormatsDisplayName(t *testing.T) {
	for _, name := range []string{"Mes Seances", `MesSeances, "Comptes"`, "MesSéances", `MesSeances <other@example.com>`} {
		t.Run(name, func(t *testing.T) {
			client := &fakeSES{}
			sender := &SESSender{Client: client, From: "no-reply@messeances.fr", FromName: name, IdentityARN: "identity", ConfigurationSet: "accounts"}
			if err := sender.Send(context.Background(), Message{Recipient: "recipient@example.com"}); err != nil {
				t.Fatal(err)
			}
			addresses, err := mail.ParseAddressList(aws.ToString(client.input.FromEmailAddress))
			if err != nil || len(addresses) != 1 || addresses[0].Name != name || addresses[0].Address != sender.From {
				t.Fatalf("unsafe or lossy sender formatting: %v, %v", addresses, err)
			}
		})
	}
}

func TestBoundedProviderBody(t *testing.T) {
	body := &boundedBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", 100))), remaining: 10}
	data, err := io.ReadAll(body)
	if len(data) != 10 || !errors.Is(err, ErrRejected) {
		t.Fatal("unbounded response")
	}
}
