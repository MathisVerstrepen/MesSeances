package accountmail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QueueClient interface {
	GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

type Feedback struct {
	Client                                                  QueueClient
	Pool                                                    *pgxpool.Pool
	Address                                                 AddressDigest
	QueueURL, TopicARN, IdentityARN, From, ConfigurationSet string
}

type feedbackEvent struct {
	EventType string `json:"eventType"`
	Mail      struct {
		Source           string              `json:"source"`
		SourceARN        string              `json:"sourceArn"`
		SendingAccountID string              `json:"sendingAccountId"`
		Destination      []string            `json:"destination"`
		Tags             map[string][]string `json:"tags"`
	} `json:"mail"`
	Bounce struct {
		Type       string              `json:"bounceType"`
		Recipients []feedbackRecipient `json:"bouncedRecipients"`
	} `json:"bounce"`
	Complaint struct {
		Recipients []feedbackRecipient `json:"complainedRecipients"`
	} `json:"complaint"`
}
type feedbackRecipient struct {
	Email string `json:"emailAddress"`
}
type suppression struct {
	digest []byte
	reason string
}

func (f *Feedback) parse(body string) ([]suppression, error) {
	var envelope struct{ Type, TopicArn, Message string }
	if decodeFeedback(body, &envelope) != nil || envelope.Type != "Notification" || envelope.TopicArn != f.TopicARN {
		return nil, ErrRejected
	}
	var event feedbackEvent
	if decodeFeedback(envelope.Message, &event) != nil {
		return nil, ErrRejected
	}
	arn := strings.Split(f.IdentityARN, ":")
	if len(arn) != 6 || event.Mail.Source != f.From || event.Mail.SourceARN != f.IdentityARN || event.Mail.SendingAccountID != arn[4] {
		return nil, ErrRejected
	}
	sets := event.Mail.Tags["ses:configuration-set"]
	if len(sets) != 1 || sets[0] != f.ConfigurationSet || len(event.Mail.Destination) == 0 || len(event.Mail.Destination) > 50 {
		return nil, ErrRejected
	}
	destinations := map[string]bool{}
	for _, address := range event.Mail.Destination {
		digest, err := f.Address(address)
		if err != nil {
			return nil, ErrRejected
		}
		destinations[string(digest)] = true
	}
	var recipients []feedbackRecipient
	reason := ""
	switch event.EventType {
	case "Bounce":
		recipients = event.Bounce.Recipients
		switch event.Bounce.Type {
		case "Permanent":
			reason = "permanent_bounce"
		case "Transient", "Undetermined":
		default:
			return nil, ErrRejected
		}
	case "Complaint":
		recipients = event.Complaint.Recipients
		reason = "complaint"
	// Other authenticated SES configuration-set events carry no suppression.
	case "Send", "Reject", "Delivery", "DeliveryDelay", "Open", "Click", "Rendering Failure", "Subscription":
		return nil, nil
	default:
		return nil, ErrRejected
	}
	if len(recipients) == 0 || len(recipients) > 50 {
		return nil, ErrRejected
	}
	var result []suppression
	for _, recipient := range recipients {
		digest, err := f.Address(recipient.Email)
		if err != nil || !destinations[string(digest)] {
			return nil, ErrRejected
		}
		if reason != "" {
			result = append(result, suppression{digest: digest, reason: reason})
		}
	}
	return result, nil
}

// JSON is bounded and duplicate keys are rejected at every depth. Unknown SES
// fields are ignored, including URLs; none is ever fetched or logged.
func decodeFeedback(raw string, dst any) error {
	if len(raw) == 0 || len(raw) > 64*1024 || !utf8.ValidString(raw) {
		return ErrRejected
	}
	d := json.NewDecoder(strings.NewReader(raw))
	if jsonValue(d, 0) != nil {
		return ErrRejected
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return ErrRejected
	}
	if json.Unmarshal([]byte(raw), dst) != nil {
		return ErrRejected
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 20 {
		return ErrRejected
	}
	t, err := d.Token()
	if err != nil {
		return ErrRejected
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return ErrRejected
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrRejected
			}
			seen[name] = true
			if jsonValue(d, depth+1) != nil {
				return ErrRejected
			}
		}
	case '[':
		for d.More() {
			if jsonValue(d, depth+1) != nil {
				return ErrRejected
			}
		}
	default:
		return ErrRejected
	}
	_, err = d.Token()
	return err
}

// Queue policies are an operator trust prerequisite: only this exact SNS topic
// may SendMessage, and only SES with source account/configuration-set may publish.
// Authenticated TLS/SigV4 SQS access replaces a public SNS signature endpoint.
func (f *Feedback) validateQueue(ctx context.Context) error {
	out, err := f.Client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: aws.String(f.QueueURL), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll}})
	if err != nil || out == nil {
		return ErrUnavailable
	}
	a := out.Attributes
	seconds, err := strconv.Atoi(a["MessageRetentionPeriod"])
	if err != nil || seconds < 60 || seconds > 7*24*3600 || (a["SqsManagedSseEnabled"] != "true" && a["KmsMasterKeyId"] == "") {
		return ErrRejected
	}
	var policy struct {
		ARN   string      `json:"deadLetterTargetArn"`
		Count json.Number `json:"maxReceiveCount"`
	}
	if decodeFeedback(a["RedrivePolicy"], &policy) != nil {
		return ErrRejected
	}
	count, err := policy.Count.Int64()
	if err != nil || count < 1 || count > 5 {
		return ErrRejected
	}
	topic := strings.Split(f.TopicARN, ":")
	if len(topic) != 6 {
		return ErrRejected
	}
	prefix := "arn:aws:sqs:" + topic[3] + ":" + topic[4] + ":"
	if !strings.HasPrefix(policy.ARN, prefix) {
		return ErrRejected
	}
	name := strings.TrimPrefix(policy.ARN, prefix)
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`).MatchString(name) {
		return ErrRejected
	}
	queueARN := prefix + f.QueueURL[strings.LastIndex(f.QueueURL, "/")+1:]
	if policy.ARN == queueARN || a["QueueArn"] != queueARN {
		return ErrRejected
	}
	dlqURL := "https://sqs." + topic[3] + ".amazonaws.com/" + topic[4] + "/" + name
	dlq, err := f.Client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: aws.String(dlqURL), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll}})
	if err != nil || dlq == nil {
		return ErrUnavailable
	}
	retention, err := strconv.Atoi(dlq.Attributes["MessageRetentionPeriod"])
	if err != nil || retention < seconds || retention > 7*24*3600 || dlq.Attributes["QueueArn"] != policy.ARN || (dlq.Attributes["SqsManagedSseEnabled"] != "true" && dlq.Attributes["KmsMasterKeyId"] == "") {
		return ErrRejected
	}
	return nil
}

func (f *Feedback) apply(ctx context.Context, records []suppression) error {
	if len(records) == 0 {
		return nil
	}
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	now := time.Now().UTC()
	for _, record := range records {
		_, err = tx.Exec(ctx, `INSERT INTO account_mail_suppressions(address_digest,reason,created_at,updated_at,expires_at) VALUES($1,$2,$3,$3,$3::timestamptz+interval '4320 hours') ON CONFLICT(address_digest) DO UPDATE SET reason=CASE WHEN account_mail_suppressions.reason='complaint' THEN 'complaint' ELSE excluded.reason END,updated_at=GREATEST(account_mail_suppressions.updated_at,excluded.updated_at),expires_at=GREATEST(account_mail_suppressions.expires_at,excluded.expires_at)`, record.digest, record.reason, now)
		if err != nil {
			return ErrUnavailable
		}
	}
	if tx.Commit(ctx) != nil {
		return ErrUnavailable
	}
	return nil
}

func (f *Feedback) poll(ctx context.Context) (accepted, rejected int, err error) {
	result, err := f.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: aws.String(f.QueueURL), MaxNumberOfMessages: 10, WaitTimeSeconds: 20, VisibilityTimeout: 60})
	if err != nil || result == nil {
		return 0, 0, ErrUnavailable
	}
	if len(result.Messages) > 10 {
		return 0, 0, ErrRejected
	}
	for _, m := range result.Messages {
		if m.Body == nil || m.ReceiptHandle == nil || len(*m.ReceiptHandle) > 4096 {
			rejected++
			continue
		}
		records, parseErr := f.parse(*m.Body)
		if parseErr != nil {
			rejected++
			continue
		} // No delete: bounded native SQS redrive to DLQ.
		if f.apply(ctx, records) != nil {
			return accepted, rejected, ErrUnavailable
		}
		if _, err = f.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(f.QueueURL), ReceiptHandle: m.ReceiptHandle}); err != nil {
			return accepted, rejected, ErrUnavailable
		}
		accepted++
	}
	return accepted, rejected, nil
}

func (f *Feedback) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := f.validateQueue(check)
		cancel()
		if err != nil {
			logger.Warn("account_mail_feedback_unavailable")
			if !pause(ctx, time.Minute) {
				return
			}
			continue
		}
		lastLog := time.Now()
		checked := lastLog
		accepted, rejected, failures := 0, 0, 0
		for ctx.Err() == nil {
			poll, cancel := context.WithTimeout(ctx, 45*time.Second)
			a, r, err := f.poll(poll)
			cancel()
			accepted += a
			rejected += r
			if err != nil {
				failures++
			}
			if time.Since(lastLog) >= time.Minute {
				logger.Info("account_mail_feedback", "processed", accepted, "rejected", rejected, "errors", failures)
				accepted, rejected, failures = 0, 0, 0
				lastLog = time.Now()
			}
			if err != nil && !pause(ctx, time.Minute) {
				return
			}
			// Revalidate redrive/retention periodically, not only on process start.
			if time.Since(checked) >= 5*time.Minute {
				break
			}
		}
	}
}
