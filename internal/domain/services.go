package domain

import (
	"slices"
	"strings"
)

// Service is the radio service a channel belongs to. The empty Service means
// unspecified; nothing is inferred from frequency, since services overlap
// (FRS and GMRS share most of their channels).
type Service string

const (
	ServiceUnspecified  Service = ""
	ServiceAmateur      Service = "amateur"
	ServiceGMRS         Service = "gmrs"
	ServiceFRS          Service = "frs"
	ServiceCB           Service = "cb"
	ServiceMURS         Service = "murs"
	ServiceMarine       Service = "marine"
	ServiceNOAA         Service = "noaa"
	ServicePublicSafety Service = "public_safety"
	ServiceOther        Service = "other"
)

var Services = []Service{ServiceAmateur, ServiceGMRS, ServiceFRS, ServiceCB, ServiceMURS, ServiceMarine, ServiceNOAA, ServicePublicSafety, ServiceOther}

func ParseService(v string) (Service, error) {
	s := Service(strings.ToLower(strings.TrimSpace(v)))
	if s == ServiceUnspecified || slices.Contains(Services, s) {
		return s, nil
	}
	names := make([]string, len(Services))
	for i, x := range Services {
		names[i] = string(x)
	}
	return "", Invalidf("unknown service %q; use %s, or empty for unspecified", v, strings.Join(names, ", "))
}

// DefaultRXOnly reports whether channels in a service are listen-only unless
// the user says otherwise: weather broadcasts and public-safety systems.
func DefaultRXOnly(s Service) bool {
	return s == ServiceNOAA || s == ServicePublicSafety
}
