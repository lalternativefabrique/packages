# meet

The server side of a video call on LiveKit, shared by every product: access
tokens scoped by role, the stage pinned for everyone (room metadata), webhook
verification, and the screen-share limits of a room.

```
go get github.com/lalternative/packages/go/meet
```

```go
m, err := meet.New(meet.Config{URL: os.Getenv("LIVEKIT_URL"), APIKey: ..., APISecret: ...})
_ = m.EnsureRoom(ctx, roomID)
token, _ := m.Token(roomID, meet.Participant{Identity: userID, Name: name, Role: meet.RoleHost})
_ = m.SetStage(ctx, roomID, meet.Stage{PinnedTrack: trackSid, PinnedBy: userID})

// LiveKit webhook endpoint
ev, err := m.Receive(r)
_, _ = m.EnforceScreenLimits(ctx, ev)
```

Roles: `host` (publish, moderate, pin), `speaker` (publish camera, microphone
and screens), `viewer` (subscribe only). Default limits: 2 screens per
participant, 4 per room, 50 participants. The product owns who may join which
room and with which role; the package never decides it.
