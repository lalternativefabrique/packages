package meet

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
)

type fakeRooms struct {
	participants []*livekit.ParticipantInfo
	muted        []string
	metadata     string
	created      *livekit.CreateRoomRequest
}

func (f *fakeRooms) CreateRoom(_ context.Context, req *livekit.CreateRoomRequest) (*livekit.Room, error) {
	f.created = req
	return &livekit.Room{Name: req.Name}, nil
}

func (f *fakeRooms) DeleteRoom(context.Context, *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error) {
	return &livekit.DeleteRoomResponse{}, nil
}

func (f *fakeRooms) ListParticipants(context.Context, *livekit.ListParticipantsRequest) (*livekit.ListParticipantsResponse, error) {
	return &livekit.ListParticipantsResponse{Participants: f.participants}, nil
}

func (f *fakeRooms) MutePublishedTrack(_ context.Context, req *livekit.MuteRoomTrackRequest) (*livekit.MuteRoomTrackResponse, error) {
	f.muted = append(f.muted, req.TrackSid)
	return &livekit.MuteRoomTrackResponse{}, nil
}

func (f *fakeRooms) UpdateRoomMetadata(_ context.Context, req *livekit.UpdateRoomMetadataRequest) (*livekit.Room, error) {
	f.metadata = req.Metadata
	return &livekit.Room{}, nil
}

func testClient(rooms *fakeRooms) *Client {
	return newClient(Config{URL: "wss://meet.test", APIKey: "key", APISecret: "a-secret-long-enough-for-hmac-signing"}, rooms)
}

func decode(t *testing.T, c *Client, token string) *auth.ClaimGrants {
	t.Helper()
	v, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatal(err)
	}
	_, grants, err := v.Verify(c.cfg.APISecret)
	if err != nil {
		t.Fatal(err)
	}
	return grants
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Config{URL: "wss://x"}); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
}

func TestTokenGrantsByRole(t *testing.T) {
	c := testClient(&fakeRooms{})
	cases := []struct {
		role    Role
		publish bool
		admin   bool
	}{
		{RoleHost, true, true},
		{RoleSpeaker, true, false},
		{RoleViewer, false, false},
	}
	for _, tc := range cases {
		token, err := c.Token("room-1", Participant{Identity: "u1", Name: "Ada", Role: tc.role})
		if err != nil {
			t.Fatal(err)
		}
		g := decode(t, c, token)
		if g.Video.Room != "room-1" || !g.Video.RoomJoin {
			t.Fatalf("%s: room grant %+v", tc.role, g.Video)
		}
		if g.Video.GetCanPublish() != tc.publish || g.Video.RoomAdmin != tc.admin {
			t.Fatalf("%s: publish=%v admin=%v", tc.role, g.Video.GetCanPublish(), g.Video.RoomAdmin)
		}
		if g.Video.GetCanUpdateOwnMetadata() {
			t.Fatalf("%s: may rewrite own metadata", tc.role)
		}
		if g.Attributes["role"] != string(tc.role) || g.Name != "Ada" || g.Identity != "u1" {
			t.Fatalf("%s: claims %+v", tc.role, g)
		}
		if tc.publish && !slices.Contains(g.Video.CanPublishSources, "screen_share") {
			t.Fatalf("%s: cannot share a screen: %v", tc.role, g.Video.CanPublishSources)
		}
	}
}

func TestTokenRejectsUnknownRoleAndMissingIdentity(t *testing.T) {
	c := testClient(&fakeRooms{})
	if _, err := c.Token("r", Participant{Identity: "u", Role: "owner"}); err == nil {
		t.Fatal("unknown role accepted")
	}
	if _, err := c.Token("r", Participant{Role: RoleHost}); err == nil {
		t.Fatal("missing identity accepted")
	}
}

func TestEnsureRoomAppliesParticipantLimit(t *testing.T) {
	rooms := &fakeRooms{}
	if err := testClient(rooms).EnsureRoom(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	if rooms.created.MaxParticipants != DefaultLimits.ParticipantsPerRoom {
		t.Fatalf("max participants %d", rooms.created.MaxParticipants)
	}
}

func TestSetStageWritesRoomMetadata(t *testing.T) {
	rooms := &fakeRooms{}
	if err := testClient(rooms).SetStage(context.Background(), "r", Stage{PinnedTrack: "TR_1", PinnedBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	var got Stage
	if err := json.Unmarshal([]byte(rooms.metadata), &got); err != nil || got.PinnedTrack != "TR_1" {
		t.Fatalf("metadata %q", rooms.metadata)
	}
}

func screen(sid string, muted bool) *livekit.TrackInfo {
	return &livekit.TrackInfo{Sid: sid, Source: livekit.TrackSource_SCREEN_SHARE, Muted: muted}
}

func published(identity, sid string) *livekit.WebhookEvent {
	return &livekit.WebhookEvent{
		Event:       webhook.EventTrackPublished,
		Room:        &livekit.Room{Name: "r"},
		Participant: &livekit.ParticipantInfo{Identity: identity},
		Track:       screen(sid, false),
	}
}

func TestEnforceScreenLimits(t *testing.T) {
	cases := []struct {
		name         string
		participants []*livekit.ParticipantInfo
		event        *livekit.WebhookEvent
		muted        bool
	}{
		{
			name: "second screen of one speaker is allowed",
			participants: []*livekit.ParticipantInfo{
				{Identity: "a", Tracks: []*livekit.TrackInfo{screen("s1", false), screen("s2", false)}},
			},
			event: published("a", "s2"),
		},
		{
			name: "third screen of one speaker is muted",
			participants: []*livekit.ParticipantInfo{
				{Identity: "a", Tracks: []*livekit.TrackInfo{screen("s1", false), screen("s2", false), screen("s3", false)}},
			},
			event: published("a", "s3"),
			muted: true,
		},
		{
			name: "fifth screen in the room is muted",
			participants: []*livekit.ParticipantInfo{
				{Identity: "a", Tracks: []*livekit.TrackInfo{screen("s1", false), screen("s2", false)}},
				{Identity: "b", Tracks: []*livekit.TrackInfo{screen("s3", false), screen("s4", false)}},
				{Identity: "c", Tracks: []*livekit.TrackInfo{screen("s5", false)}},
			},
			event: published("c", "s5"),
			muted: true,
		},
		{
			name: "muted screens do not count",
			participants: []*livekit.ParticipantInfo{
				{Identity: "a", Tracks: []*livekit.TrackInfo{screen("s1", true), screen("s2", false), screen("s3", false)}},
			},
			event: published("a", "s3"),
		},
		{
			name:  "camera tracks are ignored",
			event: &livekit.WebhookEvent{Event: webhook.EventTrackPublished, Track: &livekit.TrackInfo{Source: livekit.TrackSource_CAMERA}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rooms := &fakeRooms{participants: tc.participants}
			muted, err := testClient(rooms).EnforceScreenLimits(context.Background(), tc.event)
			if err != nil {
				t.Fatal(err)
			}
			if muted != tc.muted || (len(rooms.muted) > 0) != tc.muted {
				t.Fatalf("muted=%v calls=%v", muted, rooms.muted)
			}
		})
	}
}
