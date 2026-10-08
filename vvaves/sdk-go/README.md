# vvaves/sdk-go

The Go client for [vvaves](https://github.com/lalternativefabrique/vvaves), the
suite's text-to-speech service. Built like [`tornad/sdk-go`](../../tornad/sdk-go):
generated transport, the same options, errors and transport safety.

```go
c := sdk.New(os.Getenv("VVAVES_URL"), os.Getenv("VVAVES_KEY"),
    sdk.WithScope("synthiz"),
    sdk.WithSigning("synthiz", "https://vvaves.example.com", 0))

audio, err := c.Speak(ctx, sdk.Reading{Text: text, ID: replyID})

// Hand a browser one reading, never the key. A customer key (vvaves_key_…)
// is signed by vvaves itself; any other key signs locally under the issuer.
su, err := c.Sign(ctx, sdk.Reading{Text: text, ID: replyID})
```

The browser side is `@lalternative/vvaves-sdk-react`, which plays `su.URL`.

## What is covered

`Speak`, `SpeakStream`, `Exists`, `Prime`, `Pregenerate`, `Sign`, `Transcribe`.
`/api/v1/admin/*` (operator JWT) and `/api/keys` (console session) are excluded
by a deny list, asserted against the contract by `contract_test.go`.
Refresh with `./refresh-contract.sh`.

`signed` holds the URL signature scheme: the canonical form is pinned by a
golden test against the one deployed servers verify, and scope / id carrying
a control character are refused, since the signed string is newline-joined.

## Errors

| | |
|---|---|
| `ErrNotConfigured` | no base URL; the call was never made |
| `ErrUnauthorized` | key or signature rejected (401/403) |
| `ErrBadRequest` | arguments refused (400/422), or a scope / id the signature cannot carry |
| `ErrUnavailable` | transport failure or 5xx — degrade to no audio |
| `ErrNoAudio` | vvaves answered with nothing to play |
| `ErrNoIssuer` | local signing with no issuer configured |
| `ErrInsecureBaseURL` | plain `http://` outside the cluster, for the base URL, the public URL, or a URL vvaves signed |
| `ErrResponseTooLarge` | a response past 64 MiB (`WithMaxResponseBytes`) |
| `ErrFrameTooLarge` | a streamed piece past `MaxFrameBytes` (8 MiB) |
| `ErrAudioTooLarge` | a recording past `MaxAudioBytes` (25 MiB), refused before upload |
