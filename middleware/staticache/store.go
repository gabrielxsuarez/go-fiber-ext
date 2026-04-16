package staticache

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"strings"

	pathpkg "path"

	"github.com/andybalholm/brotli"
	"github.com/gofiber/fiber/v3"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
)

const (
	builtinTrafficAdvice     = "/.well-known/traffic-advice"
	builtinTrafficAdviceMIME = "application/trafficadvice+json"
)

var minifiableTypes = map[string]string{
	".css":  "text/css",
	".js":   "application/javascript",
	".html": "text/html",
}

type cachedFile struct {
	sourcePath string
	mimeType   string
	variants   map[string]*representation
}

func (entry *cachedFile) addVariant(name, contentEncoding string, body []byte) {
	entry.variants[name] = &representation{
		contentEncoding: contentEncoding,
		body:            body,
		etag:            computeETag(body),
	}
}

func (entry *cachedFile) addCompressedVariant(name, encoding string, compressed []byte, original []byte) {
	if len(compressed) < len(original) {
		entry.addVariant(name, encoding, compressed)
	}
}

type resolvedFile struct {
	sourcePath string
	mimeType   string
}

func cacheFile(cache *Cache, minifier *minify.M, cfg Config, fsys fs.FS, filePath string) error {
	urlPath := toURLPath(filePath)
	mimeType, minifyType, managed := classifyFile(urlPath, cfg.CustomMIMETypes)
	if !managed {
		return nil
	}

	data, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return err
	}

	if minifyType != "" && !cfg.DevMode {
		data, err = minifier.Bytes(minifyType, data)
		if err != nil {
			return fmt.Errorf("minify %s: %w", urlPath, err)
		}
	}

	entry := &cachedFile{
		sourcePath: strings.TrimPrefix(pathpkg.Clean(filePath), "/"),
		mimeType:   mimeType,
		variants:   make(map[string]*representation, 3),
	}
	entry.addVariant(encodingIdentity, "", data)

	skipCompress := cfg.DevMode

	if !skipCompress {
		gzipBody, err := compressGzip(data)
		if err != nil {
			return fmt.Errorf("gzip %s: %w", urlPath, err)
		}
		entry.addCompressedVariant(encodingGzip, encodingGzip, gzipBody, data)

		brotliBody, err := compressBrotli(data)
		if err != nil {
			return fmt.Errorf("brotli %s: %w", urlPath, err)
		}
		entry.addCompressedVariant(encodingBrotli, encodingBrotli, brotliBody, data)
	}

	cache.files[urlPath] = entry
	return nil
}

func classifyFile(urlPath string, customMIME map[string]string) (string, string, bool) {
	custom := customMIME[urlPath]

	if urlPath == builtinTrafficAdvice {
		if custom != "" {
			return custom, "", true
		}
		return builtinTrafficAdviceMIME, "", true
	}

	ext := strings.ToLower(pathpkg.Ext(urlPath))
	minifyType, canMinify := minifiableTypes[ext]
	if !canMinify {
		return "", "", false
	}

	mimeType := resolveMIME(ext)
	if custom != "" {
		mimeType = custom
	}
	return mimeType, minifyType, true
}

func registerIndexRoutes(cache *Cache, indexFile string) {
	suffix := "/" + indexFile
	for urlPath, entry := range cache.files {
		if strings.HasSuffix(urlPath, suffix) {
			dir := strings.TrimSuffix(urlPath, indexFile)
			cache.files[dir] = entry
			if strings.HasSuffix(dir, "/") && len(dir) > 1 {
				cache.files[strings.TrimSuffix(dir, "/")] = entry
			}
		}
	}
}

func (c *Cache) resolveDevFile(urlPath string) (resolvedFile, bool) {
	if entry, found := c.files[urlPath]; found {
		return resolvedFile{sourcePath: entry.sourcePath, mimeType: entry.mimeType}, true
	}

	if sourcePath, mimeType, ok := c.directManagedFile(urlPath); ok {
		return resolvedFile{sourcePath: sourcePath, mimeType: mimeType}, true
	}

	if sourcePath, mimeType, ok := c.indexManagedFile(urlPath); ok {
		return resolvedFile{sourcePath: sourcePath, mimeType: mimeType}, true
	}

	return resolvedFile{}, false
}

func (c *Cache) directManagedFile(urlPath string) (string, string, bool) {
	mimeType, _, managed := classifyFile(urlPath, c.customMIME)
	if !managed || urlPath == "/" {
		return "", "", false
	}

	return strings.TrimPrefix(urlPath, "/"), mimeType, true
}

func (c *Cache) indexManagedFile(urlPath string) (string, string, bool) {
	if c.indexFile == "" {
		return "", "", false
	}

	sourcePath := ""
	switch {
	case urlPath == "/":
		sourcePath = c.indexFile
	case strings.HasSuffix(urlPath, "/"):
		sourcePath = strings.TrimPrefix(urlPath, "/") + c.indexFile
	case pathpkg.Ext(urlPath) == "":
		sourcePath = strings.TrimPrefix(urlPath, "/") + "/" + c.indexFile
	default:
		return "", "", false
	}

	mimeType, _, managed := classifyFile("/"+sourcePath, c.customMIME)
	if !managed {
		return "", "", false
	}

	return sourcePath, mimeType, true
}

func (c *Cache) serveDevMode(ctx fiber.Ctx, resolved resolvedFile) error {
	data, err := fs.ReadFile(c.fsys, resolved.sourcePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ctx.Next()
		}
		return err
	}

	etag := computeETag(data)
	ctx.Set("Content-Type", resolved.mimeType)
	ctx.Set("ETag", etag.raw)
	applyCachePolicy(ctx, c.cacheControl)

	if matchesIfNoneMatch(ctx.Get("If-None-Match"), etag) {
		ctx.Status(fiber.StatusNotModified)
		return nil
	}

	return sendBody(ctx, data)
}

func toURLPath(filePath string) string {
	clean := pathpkg.Clean(filePath)
	if !strings.HasPrefix(clean, "/") {
		return "/" + clean
	}
	return clean
}

func resolveMIME(ext string) string {
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func computeETag(data []byte) entityTag {
	sum := sha256.Sum256(data)
	opaque := fmt.Sprintf("%x", sum[:8])
	return entityTag{
		raw:    `"` + opaque + `"`,
		opaque: opaque,
	}
}

func compressGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func compressBrotli(data []byte) ([]byte, error) {
	var buf bytes.Buffer

	w := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func newMinifier() *minify.M {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("application/javascript", js.Minify)
	m.AddFunc("text/html", html.Minify)
	return m
}
