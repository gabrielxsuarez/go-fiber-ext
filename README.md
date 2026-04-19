# go-fiber-ext

Shared utilities for projects built with [Fiber](https://gofiber.io).

It currently ships three packages: `filelog`, `middleware/requestlog`, and `middleware/staticache`.

## Installation

```bash
go get github.com/gabrielxsuarez/go-fiber-ext
```

---

## filelog

Lazy rolling file loggers with sensible defaults.

Four built-in loggers — Access, Warning, Error, and Event — are created on first use, so only the files you actually write to are created. The `Error` logger also writes to `os.Stderr`. For anything beyond the four built-in loggers, use the generic `Log` method.

Rotation is handled by [lumberjack](https://github.com/natefinch/lumberjack).

### Minimal usage

```go
fl := filelog.New("./logs")

fl.Access("| %s | %s %s (%s)", ip, method, url, duration)
fl.Warning("| %s | %s %s | %d | %q", ip, method, url, status, ua)
fl.Error("db connection failed: %v", err)   // also writes to stderr
fl.Event("deploy v2.3.1")
fl.Log("audit", "login from %s", user)      // creates audit.log on first call
```

### Custom rotation

```go
fl := filelog.New("./logs", filelog.Config{
    MaxSize:    50,  // MB per file before rotation (default: 100)
    MaxBackups: 3,   // old files to keep (default: 5)
    MaxAge:     30,  // days to retain old files (default: 0 = no limit)
})
```

| Field        | Description                                              |
| ------------ | -------------------------------------------------------- |
| `MaxSize`    | Maximum size in MB before a log file is rotated.         |
| `MaxBackups` | Maximum number of old log files to keep.                 |
| `MaxAge`     | Maximum days to retain old files (0 = no age limit).     |
| `Compress`   | Gzip rotated files. Pointer to bool (default: `true`).   |

---

## middleware/requestlog

Fiber middleware that logs HTTP requests to a `filelog.FileLog` instance.

- **Access log**: every request whose URL extension is not a known static asset (`.css`, `.js`, `.png`, etc.).
- **Warning log**: requests with status >= 400, or with an empty/unrecognised `User-Agent` (no mainstream browser token detected).

### Minimal usage

```go
fl := filelog.New("./logs")

app.Use(requestlog.New(fl))
```

### Custom skip extensions

```go
app.Use(requestlog.New(fl, requestlog.Config{
    SkipExtensions: []string{".css", ".js", ".wasm"},
}))
```

| Field            | Description                                                                 |
| ---------------- | --------------------------------------------------------------------------- |
| `SkipExtensions` | List of file extensions to exclude from the access log. If nil, uses a sensible default list. |

### Exported helpers

The functions used internally are exported so you can reuse them in custom middleware:

- `ShouldSkipAccess(ext string, skip map[string]struct{}) bool` — checks if an extension is in a skip set.
- `IsKnownBrowser(ua string) bool` — checks if a User-Agent contains a mainstream browser token (Mozilla, Chrome, Safari, Firefox, Edge, Opera).
- `DefaultSkipExtensions` — the default extension list (`[]string`).

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
