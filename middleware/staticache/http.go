package staticache

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const (
	varyAcceptEncoding  = "Accept-Encoding"
	defaultCacheControl = "no-cache, must-revalidate"
	defaultPragma       = "no-cache"
)

type entityTag struct {
	raw    string
	opaque string
}

type representation struct {
	contentEncoding string
	body            []byte
	etag            entityTag
}

func respond304(ctx fiber.Ctx, entry *cachedFile, rep *representation, cacheControl string) error {
	writeRepresentationHeaders(ctx, entry, rep, cacheControl)
	ctx.Status(fiber.StatusNotModified)
	return nil
}

func respondWithBody(ctx fiber.Ctx, entry *cachedFile, rep *representation, cacheControl string) error {
	writeRepresentationHeaders(ctx, entry, rep, cacheControl)
	return sendBody(ctx, rep.body)
}

func respondNotAcceptable(ctx fiber.Ctx) error {
	applyVary(ctx)
	ctx.Status(fiber.StatusNotAcceptable)
	return nil
}

func writeRepresentationHeaders(ctx fiber.Ctx, entry *cachedFile, rep *representation, cacheControl string) {
	ctx.Set("Content-Type", entry.mimeType)
	ctx.Set("ETag", rep.etag.raw)
	applyVary(ctx)
	applyCachePolicy(ctx, cacheControl)

	if rep.contentEncoding != "" {
		ctx.Set("Content-Encoding", rep.contentEncoding)
	}
}

func applyVary(ctx fiber.Ctx) {
	existing := string(ctx.Response().Header.Peek(fiber.HeaderVary))
	if existing == "" {
		ctx.Set(fiber.HeaderVary, varyAcceptEncoding)
		return
	}
	if varyContains(existing, varyAcceptEncoding) {
		return
	}
	ctx.Set(fiber.HeaderVary, existing+", "+varyAcceptEncoding)
}

func varyContains(headerValue, token string) bool {
	for _, part := range strings.Split(headerValue, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func applyCachePolicy(ctx fiber.Ctx, cacheControl string) {
	if cacheControl != "" {
		ctx.Set(fiber.HeaderCacheControl, cacheControl)
	}
	if strings.Contains(strings.ToLower(cacheControl), "no-cache") {
		ctx.Set(fiber.HeaderPragma, defaultPragma)
		return
	}
	ctx.Response().Header.Del(fiber.HeaderPragma)
}

func matchesIfNoneMatch(header string, current entityTag) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}

	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			return true
		}

		tag, ok := parseEntityTag(item)
		if ok && tag.opaque == current.opaque {
			return true
		}
	}

	return false
}

func parseEntityTag(value string) (entityTag, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "W/") || strings.HasPrefix(value, "w/") {
		value = strings.TrimSpace(value[2:])
	}

	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return entityTag{}, false
	}

	opaque := value[1 : len(value)-1]
	return entityTag{
		raw:    value,
		opaque: opaque,
	}, true
}

func sendBody(ctx fiber.Ctx, data []byte) error {
	ctx.Set("Content-Length", strconv.Itoa(len(data)))
	if ctx.Method() == fiber.MethodHead {
		return nil
	}
	return ctx.Send(data)
}
