package accountmail

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

var ErrTransient = errors.New("account mail temporarily unavailable")
var ErrRejected = errors.New("account mail rejected")

type SESClient interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

type SESSender struct {
	Client                                        SESClient
	From, FromName, IdentityARN, ConfigurationSet string
}

func (s *SESSender) Send(ctx context.Context, m Message) error {
	from := (&mail.Address{Name: s.FromName, Address: s.From}).String()
	_, err := s.Client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(from), FromEmailAddressIdentityArn: aws.String(s.IdentityARN), ConfigurationSetName: aws.String(s.ConfigurationSet),
		Destination: &types.Destination{ToAddresses: []string{m.Recipient}},
		Content:     &types.EmailContent{Simple: &types.Message{Subject: content(m.Subject), Body: &types.Body{Text: content(m.Text), Html: content(m.HTML)}}},
	}, func(o *sesv2.Options) { o.RetryMaxAttempts = 1 })
	return sendError(err)
}

func content(s string) *types.Content {
	return &types.Content{Data: aws.String(s), Charset: aws.String("UTF-8")}
}

// Only classifications escape this boundary, never provider text or bodies.
func sendError(err error) error {
	if err == nil {
		return nil
	}
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	var network net.Error
	if errors.As(err, &response) && (response.HTTPStatusCode() >= 500 || response.HTTPStatusCode() == 429) {
		return ErrTransient
	}
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "TooManyRequestsException", "Throttling", "ThrottlingException", "RequestTimeout", "RequestTimeoutException":
			return ErrTransient
		}
		if api.ErrorFault() == smithy.FaultServer {
			return ErrTransient
		}
		return ErrRejected
	}
	if errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrTransient
	}
	return ErrRejected
}

// NewAWS uses the SDK credential chain but pins regional service endpoints. It
// neither retrieves credentials nor performs provider requests at construction.
func NewAWS(ctx context.Context, region string) (*sesv2.Client, *sqs.Client, error) {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 25 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 10}
	client := &http.Client{Timeout: 30 * time.Second, Transport: boundedTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRejected }}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithHTTPClient(client), awsconfig.WithRetryMaxAttempts(1))
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	ses := sesv2.NewFromConfig(cfg, func(o *sesv2.Options) {
		o.BaseEndpoint = aws.String("https://email." + region + ".amazonaws.com")
		o.RetryMaxAttempts = 1
	})
	queue := sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String("https://sqs." + region + ".amazonaws.com")
		o.RetryMaxAttempts = 1
	})
	return ses, queue, nil
}

type boundedTransport struct{ base http.RoundTripper }

func (t boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: 1 << 20}
	return response, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, ErrRejected
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
