package accountmail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func testAddress(raw string) ([]byte, error) {
	if !strings.Contains(raw, "@") || strings.ContainsAny(raw, "\r\n ") {
		return nil, ErrRejected
	}
	s := sha256.Sum256([]byte(strings.ToLower(raw)))
	return s[:], nil
}
func testFeedback() *Feedback {
	return &Feedback{Address: testAddress, QueueURL: "https://sqs.eu-west-3.amazonaws.com/123456789012/accounts", TopicARN: "arn:aws:sns:eu-west-3:123456789012:feedback", IdentityARN: "arn:aws:ses:eu-west-3:123456789012:identity/example.com", From: "security@example.com", ConfigurationSet: "accounts"}
}
func feedbackBody(t *testing.T, mutate func(*feedbackEvent)) string {
	t.Helper()
	f := testFeedback()
	var e feedbackEvent
	e.EventType = "Bounce"
	e.Mail.Source = f.From
	e.Mail.SourceARN = f.IdentityARN
	e.Mail.SendingAccountID = "123456789012"
	e.Mail.Destination = []string{"person@example.com"}
	e.Mail.Tags = map[string][]string{"ses:configuration-set": {f.ConfigurationSet}}
	e.Bounce.Type = "Permanent"
	e.Bounce.Recipients = []feedbackRecipient{{Email: "person@example.com"}}
	if mutate != nil {
		mutate(&e)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"Type": "Notification", "TopicArn": f.TopicARN, "Message": string(raw), "SigningCertURL": "https://untrusted.invalid/never-fetch"})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func TestFeedbackValidation(t *testing.T) {
	f := testFeedback()
	for _, tc := range []struct {
		name   string
		change func(*feedbackEvent)
		valid  bool
		count  int
	}{
		{"permanent", nil, true, 1},
		{"transient", func(e *feedbackEvent) { e.Bounce.Type = "Transient" }, true, 0},
		{"complaint", func(e *feedbackEvent) { e.EventType = "Complaint"; e.Complaint.Recipients = e.Bounce.Recipients }, true, 1},
		{"source", func(e *feedbackEvent) { e.Mail.Source = "attacker@example.com" }, false, 0},
		{"identity", func(e *feedbackEvent) { e.Mail.SourceARN += "other" }, false, 0},
		{"account", func(e *feedbackEvent) { e.Mail.SendingAccountID = "111111111111" }, false, 0},
		{"configset", func(e *feedbackEvent) { e.Mail.Tags["ses:configuration-set"] = []string{"other"} }, false, 0},
		{"multiple sets", func(e *feedbackEvent) { e.Mail.Tags["ses:configuration-set"] = []string{"accounts", "other"} }, false, 0},
		{"foreign recipient", func(e *feedbackEvent) { e.Bounce.Recipients[0].Email = "other@example.com" }, false, 0},
		{"invalid recipient", func(e *feedbackEvent) { e.Bounce.Recipients[0].Email = "bad\n@example.com" }, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := f.parse(feedbackBody(t, tc.change))
			if (err == nil) != tc.valid || len(records) != tc.count {
				t.Fatalf("records=%d err=%v", len(records), err)
			}
		})
	}
	for _, body := range []string{strings.Replace(feedbackBody(t, nil), "arn:aws:sns:eu-west-3:123456789012:feedback", "wrong", 1), `{"Type":"Notification","Type":"Notification"}`, strings.Repeat("x", 65537), `{"Type":"SubscriptionConfirmation"}`, `null`, feedbackBody(t, nil) + `{}`} {
		if _, err := f.parse(body); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}

func TestFeedbackNamedFromHeaderKeepsPlainSourceValidation(t *testing.T) {
	f := testFeedback()
	f.From = "no-reply@messeances.fr"
	f.IdentityARN = "arn:aws:ses:eu-west-3:123456789012:identity/messeances.fr"
	wantDigest, err := testAddress("person@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct{ kind, reason string }{{"Bounce", "permanent_bounce"}, {"Complaint", "complaint"}} {
		for _, source := range []string{f.From, "attacker@example.com", `"MesSeances" <no-reply@messeances.fr>`} {
			t.Run(event.kind+"/"+source, func(t *testing.T) {
				// SES mail.source is the envelope address, not the formatted From header.
				message, err := json.Marshal(map[string]any{
					"eventType": event.kind,
					"mail": map[string]any{
						"source": source, "sourceArn": f.IdentityARN, "sendingAccountId": "123456789012",
						"destination": []string{"person@example.com"},
						"tags":        map[string][]string{"ses:configuration-set": {f.ConfigurationSet}},
						"headers":     []map[string]string{{"name": "From", "value": `"MesSeances" <no-reply@messeances.fr>`}},
						"commonHeaders": map[string]any{
							"from": []string{`"MesSeances" <no-reply@messeances.fr>`},
						},
					},
					"bounce":    map[string]any{"bounceType": "Permanent", "bouncedRecipients": []feedbackRecipient{{Email: "person@example.com"}}},
					"complaint": map[string]any{"complainedRecipients": []feedbackRecipient{{Email: "person@example.com"}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(map[string]string{"Type": "Notification", "TopicArn": f.TopicARN, "Message": string(message)})
				if err != nil {
					t.Fatal(err)
				}
				records, err := f.parse(string(body))
				if source != f.From {
					if !errors.Is(err, ErrRejected) || len(records) != 0 {
						t.Fatalf("forged source accepted: records=%d err=%v", len(records), err)
					}
					return
				}
				if err != nil || len(records) != 1 || records[0].reason != event.reason || !bytes.Equal(records[0].digest, wantDigest) {
					t.Fatalf("named sender feedback: records=%v err=%v", records, err)
				}
			})
		}
	}
}

type fakeQueue struct {
	attrs            map[string]string
	dlq              map[string]string
	messages         []types.Message
	deleted          int
	deleteErr, error error
	received         *sqs.ReceiveMessageInput
}

func (q *fakeQueue) GetQueueAttributes(_ context.Context, in *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	a := q.attrs
	if strings.HasSuffix(aws.ToString(in.QueueUrl), "/dead") {
		a = q.dlq
	}
	return &sqs.GetQueueAttributesOutput{Attributes: a}, q.error
}
func (q *fakeQueue) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	q.received = in
	return &sqs.ReceiveMessageOutput{Messages: q.messages}, q.error
}
func (q *fakeQueue) DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	q.deleted++
	return &sqs.DeleteMessageOutput{}, q.deleteErr
}
func validQueue() *fakeQueue {
	return &fakeQueue{attrs: map[string]string{"QueueArn": "arn:aws:sqs:eu-west-3:123456789012:accounts", "MessageRetentionPeriod": "345600", "SqsManagedSseEnabled": "true", "RedrivePolicy": `{"deadLetterTargetArn":"arn:aws:sqs:eu-west-3:123456789012:dead","maxReceiveCount":"5"}`}, dlq: map[string]string{"QueueArn": "arn:aws:sqs:eu-west-3:123456789012:dead", "MessageRetentionPeriod": "604800", "SqsManagedSseEnabled": "true"}}
}
func TestQueueTrustPrerequisites(t *testing.T) {
	f := testFeedback()
	q := validQueue()
	f.Client = q
	if err := f.validateQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*fakeQueue){func(q *fakeQueue) { delete(q.attrs, "RedrivePolicy") }, func(q *fakeQueue) { q.attrs["MessageRetentionPeriod"] = "1209600" }, func(q *fakeQueue) { q.attrs["SqsManagedSseEnabled"] = "false" }, func(q *fakeQueue) { q.dlq["MessageRetentionPeriod"] = "1209600" }, func(q *fakeQueue) {
		q.attrs["RedrivePolicy"] = `{"deadLetterTargetArn":"arn:aws:sqs:eu-west-3:111111111111:dead","maxReceiveCount":5}`
	}, func(q *fakeQueue) {
		q.attrs["RedrivePolicy"] = `{"deadLetterTargetArn":"arn:aws:sqs:eu-west-3:123456789012:dead","maxReceiveCount":6}`
	}} {
		q := validQueue()
		change(q)
		f.Client = q
		if err := f.validateQueue(context.Background()); err == nil {
			t.Fatal("unsafe queue accepted")
		}
	}
}
func TestPoisonNotDeletedAndTransientIgnored(t *testing.T) {
	f := testFeedback()
	q := validQueue()
	f.Client = q
	q.messages = []types.Message{{Body: aws.String("poison"), ReceiptHandle: aws.String("receipt")}, {Body: aws.String(feedbackBody(t, func(e *feedbackEvent) { e.Bounce.Type = "Transient" })), ReceiptHandle: aws.String("receipt2")}}
	a, r, err := f.poll(context.Background())
	if err != nil || a != 1 || r != 1 || q.deleted != 1 {
		t.Fatal("poison handling")
	}
	if q.received.WaitTimeSeconds != 20 || q.received.MaxNumberOfMessages != 10 || q.received.VisibilityTimeout != 60 {
		t.Fatal("unbounded polling")
	}
	q.error = errors.New("sensitive provider body")
	if _, _, err = f.poll(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unsafe error")
	}
}
