# @lalternative/meet

The video call UI shared by L'Alternative apps, on LiveKit: several speakers,
several screens per speaker, `grid` / `stage` / `screens` layouts, and a stage
the host pins for everyone. The product's server issues the token with
`go/meet`.

```tsx
import '@livekit/components-styles'
import '@lalternative/meet/styles.css'
import { MeetRoom } from '@lalternative/meet'

<MeetRoom serverUrl={url} token={token} onLeave={close} onPin={(sid) => api.pin(roomId, sid)} />
```

`onPin` must go through the product's server, which writes the stage with
`go/meet`: participants cannot rewrite room metadata themselves.
