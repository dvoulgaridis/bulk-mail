package smtp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

var ErrInvalidInput = errors.New("invalid SMTP detection input")

type Candidate struct {
	Host    string
	Port    int
	TLSMode string
}

type Result struct {
	Endpoint *Endpoint `json:"endpoint"`
}

func Detect(ctx context.Context, email string, connectTimeout time.Duration) (Result, error) {
	normalizedEmail, err := validation.NormalizeEmail(email)
	if err != nil {
		return Result{}, fmt.Errorf("%w: email: %v", ErrInvalidInput, err)
	}
	at := strings.LastIndex(normalizedEmail, "@")
	if at < 0 || at == len(normalizedEmail)-1 {
		return Result{}, fmt.Errorf("%w: email domain is required", ErrInvalidInput)
	}
	domain := normalizeHost(normalizedEmail[at+1:])
	if domain == "" {
		return Result{}, fmt.Errorf("%w: email domain is required", ErrInvalidInput)
	}

	candidates, err := discoverCandidates(ctx, domain)
	if err != nil {
		return Result{}, err
	}
	for _, candidate := range candidates {
		client, _, _, err := openClient(ctx, candidate.Endpoint(), connectTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			continue
		}
		_ = client.Close()
		endpoint := candidate.Endpoint()
		return Result{Endpoint: &endpoint}, nil
	}
	return Result{}, nil
}

func discoverCandidates(ctx context.Context, domain string) ([]Candidate, error) {
	implicitTLS, suppressTLS, err := lookupSRVCandidates(ctx, "submissions", domain, "tls")
	if err != nil {
		return nil, err
	}
	startTLS, suppressStartTLS, err := lookupSRVCandidates(ctx, "submission", domain, "starttls")
	if err != nil {
		return nil, err
	}
	mxHosts, err := lookupMXHosts(ctx, domain)
	if err != nil {
		return nil, err
	}

	candidates := append(implicitTLS, startTLS...)
	candidates = append(candidates, candidatesFromRules(domain, mxHosts)...)
	candidates = append(candidates,
		Candidate{Host: "smtp." + domain, Port: 587, TLSMode: "starttls"},
		Candidate{Host: "smtp." + domain, Port: 465, TLSMode: "tls"},
		Candidate{Host: "mail." + domain, Port: 587, TLSMode: "starttls"},
		Candidate{Host: "mail." + domain, Port: 465, TLSMode: "tls"},
	)
	return deduplicateCandidates(filterSuppressed(candidates, suppressTLS, suppressStartTLS)), nil
}

func lookupSRVCandidates(
	ctx context.Context,
	service string,
	domain string,
	tlsMode string,
) ([]Candidate, bool, error) {
	_, records, err := net.DefaultResolver.LookupSRV(ctx, service, "tcp", domain)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, false, nil
	}
	for _, record := range records {
		if record.Target == "." {
			return nil, true, nil
		}
	}
	candidates := make([]Candidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, Candidate{
			Host:    normalizeHost(record.Target),
			Port:    int(record.Port),
			TLSMode: tlsMode,
		})
	}
	return candidates, false, nil
}

func lookupMXHosts(ctx context.Context, domain string) ([]string, error) {
	records, err := net.DefaultResolver.LookupMX(ctx, domain)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	hosts := make([]string, 0, len(records))
	for _, record := range records {
		hosts = append(hosts, normalizeHost(record.Host))
	}
	return hosts, nil
}

func filterSuppressed(candidates []Candidate, suppressTLS, suppressStartTLS bool) []Candidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		mode := strings.ToLower(candidate.TLSMode)
		if (mode == "tls" && suppressTLS) || (mode == "starttls" && suppressStartTLS) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func deduplicateCandidates(candidates []Candidate) []Candidate {
	seen := make(map[string]struct{}, len(candidates))
	result := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate.Host = normalizeHost(candidate.Host)
		candidate.TLSMode = strings.ToLower(strings.TrimSpace(candidate.TLSMode))
		if candidate.Host == "" || candidate.Port < 1 || candidate.Port > 65535 {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", candidate.Host, candidate.Port, candidate.TLSMode)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func (candidate Candidate) Endpoint() Endpoint {
	return Endpoint{
		Host:    candidate.Host,
		Port:    candidate.Port,
		TLSMode: candidate.TLSMode,
	}
}
