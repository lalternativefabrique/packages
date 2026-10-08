# @lalternative/vvaves-sdk-react

Plays a vvaves reading in the browser, straight from vvaves, on a URL the
application's server signed. The server never relays audio: it primes the
opening when the text is produced, signs one URL per press, and the bytes go
from vvaves to the listener.

```tsx
import { speakSource, useVoicePlayback } from '@lalternative/vvaves-sdk-react'

function PlayButton({ messageId }: { messageId: string }) {
  const resolve = useCallback(async () => {
    const res = await fetch(`${API}/messages/${messageId}/audio`, { credentials: 'include' })
    const { url, text } = await res.json()
    return speakSource({ url, text, scope: 'chat-message', id: messageId })
  }, [messageId])
  const { state, toggle } = useVoicePlayback(resolve)
  if (state === 'unavailable') return null
  return <button onClick={toggle}>{state === 'playing' ? 'Pause' : 'Play'}</button>
}
```

The server side is the Go `client` package of this module: `client.New` with
the application's `Key` for `PrimeOpening`, and `signed.NewSigner` with the
same key for the URL the endpoint above returns.
