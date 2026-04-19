package staticache

import (
	"io/fs"
	"os"

	"github.com/gofiber/fiber/v3"
)

type Config struct {
	DevMode         bool
	CustomMIMETypes map[string]string
	IndexFile       string
	Next            func(fiber.Ctx) bool
}

type Cache struct {
	fsys         fs.FS
	files        map[string]*cachedFile
	passthrough  map[string]string // urlPath → sourcePath (known files not loaded into memory)
	cacheControl string
	indexFile    string
	customMIME   map[string]string
	next         func(fiber.Ctx) bool
	devMode      bool
	handler      fiber.Handler
}

func New(root string, configs ...Config) (*Cache, error) {
	return NewFS(os.DirFS(root), configs...)
}

func NewFS(fsys fs.FS, configs ...Config) (*Cache, error) {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}

	cache := &Cache{
		fsys:         fsys,
		files:        make(map[string]*cachedFile),
		passthrough:  make(map[string]string),
		cacheControl: defaultCacheControl,
		indexFile:    cfg.IndexFile,
		customMIME:   cfg.CustomMIMETypes,
		next:         cfg.Next,
		devMode:      cfg.DevMode,
	}

	minifier := newMinifier()

	err := fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return cacheFile(cache, minifier, cfg, fsys, filePath)
	})
	if err != nil {
		return nil, err
	}

	if cfg.IndexFile != "" {
		registerIndexRoutes(cache, cfg.IndexFile)
	}

	cache.handler = cache.newHandler()
	return cache, nil
}

func (c *Cache) Handler() fiber.Handler {
	return c.handler
}

func (c *Cache) servePassthrough(ctx fiber.Ctx) error {
	sourcePath, known := c.passthrough[ctx.Path()]
	if !known {
		return ctx.Next()
	}

	info, err := fs.Stat(c.fsys, sourcePath)
	if err != nil {
		return ctx.Next()
	}

	etag := computeStatETag(info)
	ctx.Set("ETag", etag.raw)
	applyCachePolicy(ctx, c.cacheControl)

	if matchesIfNoneMatch(ctx.Get("If-None-Match"), etag) {
		ctx.Status(fiber.StatusNotModified)
		return nil
	}

	return ctx.Next()
}

func (c *Cache) newHandler() fiber.Handler {
	return func(ctx fiber.Ctx) error {
		if c.next != nil && c.next(ctx) {
			return ctx.Next()
		}

		if ctx.Method() != fiber.MethodGet && ctx.Method() != fiber.MethodHead {
			return ctx.Next()
		}

		if c.devMode {
			resolved, ok := c.resolveDevFile(ctx.Path())
			if !ok {
				return c.servePassthrough(ctx)
			}
			return c.serveDevMode(ctx, resolved)
		}

		entry, found := c.files[ctx.Path()]
		if !found {
			return c.servePassthrough(ctx)
		}

		rep, ok := selectRepresentation(ctx.Get(varyAcceptEncoding), entry)
		if !ok {
			return respondNotAcceptable(ctx)
		}

		if matchesIfNoneMatch(ctx.Get("If-None-Match"), rep.etag) {
			return respond304(ctx, entry, rep, c.cacheControl)
		}

		return respondWithBody(ctx, entry, rep, c.cacheControl)
	}
}
