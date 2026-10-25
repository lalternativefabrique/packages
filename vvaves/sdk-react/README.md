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

`toggle` pauses and resumes. For a full player the hook also returns
`position`, `buffered` and `duration` (seconds; `duration` is `null` until the
stream ends), `seek(seconds)` clamped to what is decoded, `skip(delta)`, and
`rate` / `setRate(rate)` within 0.5..3.

## Following along

`useReadAlong` highlights, in the page the listener reads, the sentence the
voice is on, and scrolls it back into view while playing:

```tsx
const playback = useVoicePlayback(resolve)
const article = useRef<HTMLElement>(null)
useReadAlong(playback, article)
return <article ref={article}>{rendered}</article>
```

```css
::highlight(vvaves-reading) { background-color: #fde68a; }
```

The spoken text and the rendered page are matched on their letters, so
Markdown, list markers and section names read aloud do not get in the way.
Position is estimated from the reading's progress, exact to the sentence on a
cached reading, approximate on the first, streamed listen. Drawn with the CSS
Custom Highlight API, which leaves the DOM alone. `name` changes the highlight,
`follow: false` stops the scrolling.

The server side is the Go `client` package of this module: `client.New` with
the application's `Key` for `PrimeOpening`, and `signed.NewSigner` with the
same key for the URL the endpoint above returns.
