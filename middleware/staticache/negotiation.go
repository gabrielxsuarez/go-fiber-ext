package staticache

import (
	"strconv"
	"strings"
)

const (
	encodingIdentity = "identity"
	encodingGzip     = "gzip"
	encodingBrotli   = "br"

	defaultQValue = 1000
	qUnspecified  = -1
)

type acceptEncoding struct {
	present  bool
	br       int
	gzip     int
	identity int
	wildcard int
}

func selectRepresentation(header string, entry *cachedFile) (*representation, bool) {
	enc := parseAcceptEncoding(header)
	var best *representation
	bestQuality := -1

	for _, name := range []string{encodingBrotli, encodingGzip, encodingIdentity} {
		rep := entry.variants[name]
		if rep == nil {
			continue
		}

		quality := enc.quality(name)
		if quality <= 0 {
			continue
		}
		if best == nil || quality > bestQuality {
			best = rep
			bestQuality = quality
		}
	}

	return best, best != nil
}

func parseAcceptEncoding(header string) acceptEncoding {
	header = strings.TrimSpace(header)
	if header == "" {
		return acceptEncoding{}
	}

	enc := acceptEncoding{
		present:  true,
		br:       qUnspecified,
		gzip:     qUnspecified,
		identity: qUnspecified,
		wildcard: qUnspecified,
	}

	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		parts := strings.Split(item, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		if name == "" {
			continue
		}

		quality := defaultQValue
		valid := true

		for _, param := range parts[1:] {
			key, value, ok := strings.Cut(param, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}

			quality, valid = parseQValue(strings.TrimSpace(value))
			break
		}

		if !valid {
			continue
		}

		switch name {
		case encodingBrotli:
			enc.br = quality
		case encodingGzip:
			enc.gzip = quality
		case encodingIdentity:
			enc.identity = quality
		case "*":
			enc.wildcard = quality
		}
	}

	return enc
}

func (ae acceptEncoding) quality(name string) int {
	if !ae.present {
		if name == encodingIdentity {
			return defaultQValue
		}
		return 0
	}

	explicit := qUnspecified
	switch name {
	case encodingBrotli:
		explicit = ae.br
	case encodingGzip:
		explicit = ae.gzip
	case encodingIdentity:
		explicit = ae.identity
	}

	if explicit != qUnspecified {
		return explicit
	}

	if ae.wildcard != qUnspecified {
		return ae.wildcard
	}

	if name == encodingIdentity {
		return defaultQValue
	}

	return 0
}

func parseQValue(value string) (int, bool) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil || f < 0 || f > 1 {
		return 0, false
	}
	return int(f*defaultQValue + 0.5), true
}
