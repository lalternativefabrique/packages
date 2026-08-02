package outbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/nats-io/nats.go"
)

const (
	StreamName = "WEBHOOK_OUTBOX"
	subjectPat = "outbox.webhook.>"
)

// jobPayload is the wire shape pushed to JetStream. The body is base64'd to
// keep the JSON envelope debuggable via `nats stream view`.
type jobPayload struct {
	DeliveryID    string `json:"deliveryId"`
	EndpointID    string `json:"endpointId"`
	TenantID      string `json:"tenantId"`
	URL           string `json:"url"`
	Secret        string `json:"secret"`
	EventType     string `json:"eventType"`
	SourceEventID string `json:"sourceEventId"`
	PayloadBase64 string `json:"payloadBase64"`
}

type NATSOutboxPublisher struct {
	js nats.JetStreamContext
}

func NewNATSOutboxPublisher(js nats.JetStreamContext) (*NATSOutboxPublisher, error) {
	if err := EnsureStream(js); err != nil {
		return nil, err
	}
	return &NATSOutboxPublisher{js: js}, nil
}

func EnsureStream(js nats.JetStreamContext) error {
	if _, err := js.StreamInfo(StreamName); err == nil {
		return nil
	}
	_, err := js.AddStream(&nats.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{subjectPat},
		Storage:   nats.FileStorage,
		Retention: nats.WorkQueuePolicy,
		MaxAge:    0,
	})
	if err != nil {
		if strings.Contains(err.Error(), "already in use") || strings.Contains(err.Error(), "name already") {
			return nil
		}
		return fmt.Errorf("create webhook outbox stream: %w", err)
	}
	return nil
}

func (p *NATSOutboxPublisher) Publish(ctx context.Context, job providers.DeliveryJob) error {
	payload := jobPayload{
		DeliveryID:    job.DeliveryID,
		EndpointID:    job.EndpointID,
		TenantID:      job.TenantID,
		URL:           job.URL,
		Secret:        job.Secret,
		EventType:     job.EventType,
		SourceEventID: job.SourceEventID,
		PayloadBase64: base64.StdEncoding.EncodeToString(job.Payload),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	subject := "outbox.webhook." + job.DeliveryID
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	msg.Header.Set("Delivery-ID", job.DeliveryID)
	msg.Header.Set("Endpoint-ID", job.EndpointID)
	msg.Header.Set("Tenant-ID", job.TenantID)
	msg.Header.Set("Event-Type", job.EventType)

	if _, err := p.js.PublishMsg(msg, nats.MsgId(job.DeliveryID)); err != nil {
		return fmt.Errorf("publish outbox: %w", err)
	}
	return nil
}

func DecodeJob(data []byte) (providers.DeliveryJob, error) {
	var p jobPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return providers.DeliveryJob{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(p.PayloadBase64)
	if err != nil {
		return providers.DeliveryJob{}, fmt.Errorf("decode payload: %w", err)
	}
	return providers.DeliveryJob{
		DeliveryID:    p.DeliveryID,
		EndpointID:    p.EndpointID,
		TenantID:      p.TenantID,
		URL:           p.URL,
		Secret:        p.Secret,
		EventType:     p.EventType,
		SourceEventID: p.SourceEventID,
		Payload:       raw,
	}, nil
}
