package auth

type Principal struct {
	Subject string            `json:"subject"`
	Name    string            `json:"name,omitempty"`
	Roles   []string          `json:"roles,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

func (p Principal) HasRole(role string) bool {
	for _, current := range p.Roles {
		if current == role {
			return true
		}
	}
	return false
}

func (p Principal) HasAnyRole(roles ...string) bool {
	for _, role := range roles {
		if p.HasRole(role) {
			return true
		}
	}
	return false
}

func HasRole(principal Principal, role string) bool {
	return principal.HasRole(role)
}

func HasAnyRole(principal Principal, roles ...string) bool {
	return principal.HasAnyRole(roles...)
}
