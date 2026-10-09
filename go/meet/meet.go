// Package meet is the server side of a video call on LiveKit, shared by every
// product: access tokens scoped by role, the shared stage kept in room
// metadata, webhooks, and the screen-share limits a room enforces.
package meet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type Role string

const (
	RoleHost    Role = "host"
	RoleSpeaker Role = "speaker"
	RoleViewer  Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleHost || r == RoleSpeaker || r == RoleViewer
}

type Limits struct {
	ScreensPerParticipant int
	ScreensPerRoom        int
	ParticipantsPerRoom   uint32
}

var DefaultLimits = Limits{ScreensPerParticipant: 2, ScreensPerRoom: 4, ParticipantsPerRoom: 50}

type Config struct {
	URL       string
	APIKey    string
	APISecret string
	TokenTTL  time.Duration
	Limits    Limits
}

type Participant struct {
	Identity string
	Name     string
	Role     Role
}

type Stage struct {
	PinnedTrack string `json:"pinnedTrack,omitempty"`
	PinnedBy    string `json:"pinnedBy,omitempty"`
}

type roomService interface {
	CreateRoom(ctx context.Context, req *livekit.CreateRoomRequest) (*livekit.Room, error)
	DeleteRoom(ctx context.Context, req *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error)
	ListParticipants(ctx context.Context, req *livekit.ListParticipantsRequest) (*livekit.ListParticipantsResponse, error)
	MutePublishedTrack(ctx context.Context, req *livekit.MuteRoomTrackRequest) (*livekit.MuteRoomTrackResponse, error)
	UpdateRoomMetadata(ctx context.Context, req *livekit.UpdateRoomMetadataRequest) (*livekit.Room, error)
}

type Client struct {
	cfg   Config
	rooms roomService
	keys  auth.KeyProvider
}

var ErrNotConfigured = errors.New("meet: url, api key and api secret are required")

func New(cfg Config) (*Client, error) {
	if cfg.URL == "" || cfg.APIKey == "" || cfg.APISecret == "" {
		return nil, ErrNotConfigured
	}
	return newClient(cfg, lksdk.NewRoomServiceClient(cfg.URL, cfg.APIKey, cfg.APISecret)), nil
}

func newClient(cfg Config, rooms roomService) *Client {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 6 * time.Hour
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits
	}
	return &Client{cfg: cfg, rooms: rooms, keys: auth.NewSimpleKeyProvider(cfg.APIKey, cfg.APISecret)}
}

func (c *Client) URL() string { return c.cfg.URL }

func (c *Client) Limits() Limits { return c.cfg.Limits }

func (c *Client) Token(room string, p Participant) (string, error) {
	if room == "" || p.Identity == "" {
		return "", errors.New("meet: room and identity are required")
	}
	if !p.Role.Valid() {
		return "", fmt.Errorf("meet: unknown role %q", p.Role)
	}
	at := auth.NewAccessToken(c.cfg.APIKey, c.cfg.APISecret).
		SetIdentity(p.Identity).
		SetName(p.Name).
		SetValidFor(c.cfg.TokenTTL).
		SetAttributes(map[string]string{"role": string(p.Role)}).
		SetVideoGrant(grantFor(room, p.Role))
	return at.ToJWT()
}

func grantFor(room string, role Role) *auth.VideoGrant {
	grant := &auth.VideoGrant{RoomJoin: true, Room: room}
	grant.SetCanSubscribe(true)
	grant.SetCanPublishData(true)
	grant.SetCanUpdateOwnMetadata(false)
	switch role {
	case RoleViewer:
		grant.SetCanPublish(false)
	default:
		grant.SetCanPublish(true)
		grant.SetCanPublishSources([]livekit.TrackSource{
			livekit.TrackSource_CAMERA,
			livekit.TrackSource_MICROPHONE,
			livekit.TrackSource_SCREEN_SHARE,
			livekit.TrackSource_SCREEN_SHARE_AUDIO,
		})
	}
	grant.RoomAdmin = role == RoleHost
	return grant
}

func (c *Client) EnsureRoom(ctx context.Context, room string) error {
	_, err := c.rooms.CreateRoom(ctx, &livekit.CreateRoomRequest{
		Name:             room,
		EmptyTimeout:     300,
		DepartureTimeout: 60,
		MaxParticipants:  c.cfg.Limits.ParticipantsPerRoom,
	})
	return err
}

func (c *Client) EndRoom(ctx context.Context, room string) error {
	_, err := c.rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: room})
	return err
}

func (c *Client) SetStage(ctx context.Context, room string, stage Stage) error {
	body, err := json.Marshal(stage)
	if err != nil {
		return err
	}
	_, err = c.rooms.UpdateRoomMetadata(ctx, &livekit.UpdateRoomMetadataRequest{Room: room, Metadata: string(body)})
	return err
}

func (c *Client) Receive(r *http.Request) (*livekit.WebhookEvent, error) {
	return webhook.ReceiveWebhookEvent(r, c.keys)
}

// EnforceScreenLimits mutes a screen share published past the room's limits.
// LiveKit cannot cap screen tracks itself, so the check runs on the
// track_published webhook, after the track is already live.
func (c *Client) EnforceScreenLimits(ctx context.Context, ev *livekit.WebhookEvent) (bool, error) {
	if ev.GetEvent() != webhook.EventTrackPublished || ev.GetTrack().GetSource() != livekit.TrackSource_SCREEN_SHARE {
		return false, nil
	}
	room := ev.GetRoom().GetName()
	res, err := c.rooms.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: room})
	if err != nil {
		return false, err
	}
	publisher := ev.GetParticipant().GetIdentity()
	own, total := 0, 0
	for _, p := range res.GetParticipants() {
		for _, t := range p.GetTracks() {
			if t.GetSource() != livekit.TrackSource_SCREEN_SHARE || t.GetMuted() {
				continue
			}
			total++
			if p.GetIdentity() == publisher {
				own++
			}
		}
	}
	if own <= c.cfg.Limits.ScreensPerParticipant && total <= c.cfg.Limits.ScreensPerRoom {
		return false, nil
	}
	_, err = c.rooms.MutePublishedTrack(ctx, &livekit.MuteRoomTrackRequest{
		Room:     room,
		Identity: publisher,
		TrackSid: ev.GetTrack().GetSid(),
		Muted:    true,
	})
	return err == nil, err
}
