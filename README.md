# go-fiber-ext

Shared utilities for projects built with [Fiber](https://gofiber.io).

Right now it ships one package — `middleware/staticache` — but more may be added over time.

## Installation

```bash
go get github.com/gabrielxsuarez/go-fiber-ext
```

---

## middleware/staticache

A production-grade static file cache for Fiber.

Reads your static files at startup, minifies CSS/JS/HTML, pre-compresses them with gzip and brotli, then serves everything from memory with proper ETags, `Vary`, and content negotiation. No disk I/O at request time.

### What it does

- Minifies CSS, JS, and HTML using [tdewolff/minify](https://github.com/tdewolff/minify)
- Pre-compresses with gzip and brotli (only keeps the compressed variant when it's actually smaller)
- Serves the best encoding based on the client's `Accept-Encoding` header, including q-value negotiation
- Generates ETags and responds `304 Not Modified` when appropriate
- Sets `Vary: Accept-Encoding` (merges with existing headers, never duplicates)
- Applies `Cache-Control: no-cache, must-revalidate` + `Pragma: no-cache` by default
- Serves `.well-known/traffic-advice` with the correct `application/trafficadvice+json` content type
- Maps `index.html` files to their parent directory paths (both `/docs/` and `/docs`)
- Dev mode: skips minification and compression, reads files from disk on every request so you can edit and refresh without restarting

### What it doesn't do

- Serve unmanaged file types (images, fonts, etc.) — those fall through to the next handler
- Handle range requests
- Do directory listing
- Cache dynamically generated content

### Minimal usage

```go
cache, err := staticache.New("./static")
if err != nil {
    log.Fatal(err)
}
app.Use(cache.Handler())
```

### Full configuration

```go
cache, err := staticache.New("./static", staticache.Config{
    DevMode:         os.Getenv("ENV") == "development",
    CustomMIMETypes: map[string]string{
        "/manifest.json": "application/manifest+json",
    },
    IndexFile:       "index.html",
    Next: func(c fiber.Ctx) bool {
        return c.Path() == "/healthz"
    },
})
if err != nil {
    log.Fatal(err)
}
app.Use(cache.Handler())
```

| Field             | Description                                                              |
| ----------------- | ------------------------------------------------------------------------ |
| `DevMode`         | Skips minification and compression. Reads from disk on every request.    |
| `CustomMIMETypes` | Override content types by URL path.                                      |
| `IndexFile`       | Register directory routes for this index file (e.g. `/docs/` → `docs/index.html`). |
| `Next`            | Skip the handler when this function returns `true`.                      |

You can also use `staticache.NewFS(fsys, ...)` to pass any `fs.FS` instead of a directory path.
