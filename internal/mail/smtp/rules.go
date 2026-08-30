package smtp

import "strings"

type Rule struct {
	Domains    []string
	MXPatterns []string
	Candidates []Candidate
}

var discoveryRules = []Rule{
	{
		Domains:    []string{"gmail.com", "googlemail.com"},
		MXPatterns: []string{"*.google.com"},
		Candidates: []Candidate{{Host: "smtp.gmail.com", Port: 587, TLSMode: "starttls"}},
	},
	{
		Domains:    []string{"fastmail.com", "fastmail.fm"},
		MXPatterns: []string{"*.messagingengine.com"},
		Candidates: []Candidate{{Host: "smtp.fastmail.com", Port: 465, TLSMode: "tls"}},
	},
	{
		Domains: []string{
			"outlook.com",
			"hotmail.com",
			"live.com",
			"msn.com",
		},
		MXPatterns: []string{"*.protection.outlook.com"},
		Candidates: []Candidate{{Host: "smtp-mail.outlook.com", Port: 587, TLSMode: "starttls"}},
	},
	{
		Domains:    []string{"yahoo.com", "ymail.com", "rocketmail.com"},
		MXPatterns: []string{"*.yahoodns.net"},
		Candidates: []Candidate{{Host: "smtp.mail.yahoo.com", Port: 587, TLSMode: "starttls"}},
	},
}

func candidatesFromRules(domain string, mxHosts []string) []Candidate {
	var candidates []Candidate
	for _, rule := range discoveryRules {
		if matchesAny(domain, rule.Domains) || matchesMXRule(mxHosts, rule.MXPatterns) {
			candidates = append(candidates, rule.Candidates...)
		}
	}
	return candidates
}

func matchesMXRule(hosts, patterns []string) bool {
	for _, host := range hosts {
		if matchesAny(host, patterns) {
			return true
		}
	}
	return false
}

func matchesAny(value string, patterns []string) bool {
	value = normalizeHost(value)
	for _, pattern := range patterns {
		pattern = normalizeHost(pattern)
		if value == pattern {
			return true
		}
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(value, pattern[1:]) {
			return true
		}
	}
	return false
}

func normalizeHost(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
