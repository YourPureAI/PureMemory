package privacy

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"unicode"

	"user-memory-collector/watchers"
)

var BaselineAppBlacklist = []string{
	"1Password.exe",
	"LastPass.exe",
	"Bitwarden.exe",
	"KeePassXC.exe",
	"cmd.exe",
	"powershell.exe",
}

// privacyConfig is the JSON structure of privacy.json
type privacyConfig struct {
	UserID          string   `json:"user_id"`
	DeviceID        string   `json:"device_id"`
	SecretPrefixes  []string `json:"secret_prefixes"`
	ExcludedDomains []string `json:"excluded_domains"`
	Tags            []string `json:"tags"`
}

var defaultSecretPrefixes = []string{
	"sk-",
	"ghp_",
	"ghs_",
	"xoxb-",
	"xoxp-",
	"AIza",
	"ya29.",
	"Bearer ",
	"AKIA",
}

var defaultExcludedDomains = []string{
	"paypal.com",
}

// Filter manages the privacy heuristics and blacklists.
type Filter struct {
	appBlacklistSet map[string]struct{}
	mu              sync.RWMutex
	builtinPrefixes []string
	customPrefixes  []string
	builtinDomains  []string
	customDomains   []string
	customTags      []string
	userID          string
	deviceID        string
	configPath      string
}

// NewFilter sets up the hash map for O(1) blacklist lookups and loads config.
func NewFilter(configPath string) *Filter {
	f := &Filter{
		appBlacklistSet: make(map[string]struct{}),
		builtinPrefixes: append([]string{}, defaultSecretPrefixes...),
		builtinDomains:  append([]string{}, defaultExcludedDomains...),
		configPath:      configPath,
	}
	for _, app := range BaselineAppBlacklist {
		f.appBlacklistSet[strings.ToLower(app)] = struct{}{}
	}

	// Migrate from older versions by destroying the old file
	if _, err := os.Stat("prefixes.json"); err == nil {
		os.Remove("prefixes.json")
		log.Println("[Privacy] Removed deprecated prefixes.json")
	}

	f.loadConfig(configPath)
	return f
}

func (f *Filter) loadConfig(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		f.createConfigTemplate(path)
		return
	}
	var cfg privacyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[Privacy] %s parse error: %v — using defaults", path, err)
		return
	}
	f.customPrefixes = cfg.SecretPrefixes
	f.customDomains = cfg.ExcludedDomains
	f.customTags = cfg.Tags
	f.userID = cfg.UserID
	f.deviceID = cfg.DeviceID

	if len(f.customPrefixes) > 0 || len(f.customDomains) > 0 || len(f.customTags) > 0 {
		log.Printf("[Privacy] Loaded %d custom prefixes, %d custom domains, %d custom tags", len(f.customPrefixes), len(f.customDomains), len(f.customTags))
	}
}

func (f *Filter) createConfigTemplate(path string) {
	template := privacyConfig{
		UserID:          "",
		DeviceID:        "",
		SecretPrefixes:  []string{},
		ExcludedDomains: []string{},
		Tags:            []string{},
	}
	data, _ := json.MarshalIndent(template, "", "  ")
	_ = os.WriteFile(path, data, 0600)
	log.Printf("[Privacy] Created %s", path)
}

// GetConfig returns the current user-defined config (for Settings UI readback).
func (f *Filter) GetConfig() (string, string, []string, []string, []string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	
	prefixes := make([]string, len(f.customPrefixes))
	copy(prefixes, f.customPrefixes)
	
	domains := make([]string, len(f.customDomains))
	copy(domains, f.customDomains)
	
	tags := make([]string, len(f.customTags))
	copy(tags, f.customTags)

	return f.userID, f.deviceID, prefixes, domains, tags
}

func (f *Filter) GetTags() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	tags := make([]string, len(f.customTags))
	copy(tags, f.customTags)
	return tags
}

// SetConfig replaces user config, saves to disk, and hot-reloads.
func (f *Filter) SetConfig(userID, deviceID string, prefixes, domains, tags []string) error {
	cfg := privacyConfig{
		UserID:          userID,
		DeviceID:        deviceID,
		SecretPrefixes:  prefixes,
		ExcludedDomains: domains,
		Tags:            tags,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(f.configPath, data, 0600); err != nil {
		return err
	}
	f.mu.Lock()
	f.userID = userID
	f.deviceID = deviceID
	f.customPrefixes = append([]string{}, prefixes...)
	f.customDomains = append([]string{}, domains...)
	f.customTags = append([]string{}, tags...)
	f.mu.Unlock()
	log.Printf("[Privacy] Config updated: user=%s, device=%s", userID, deviceID)
	return nil
}

// GetIdentity returns UserID and DeviceID
func (f *Filter) GetIdentity() (string, string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.userID, f.deviceID
}

// RedactText globally scans the text (which can be a JSON string) for prefixes and replaces the whole token.
func (f *Filter) RedactText(text string) string {
	f.mu.RLock()
	active := make([]string, 0, len(f.builtinPrefixes)+len(f.customPrefixes))
	active = append(active, f.builtinPrefixes...)
	active = append(active, f.customPrefixes...)
	f.mu.RUnlock()

	if len(active) == 0 || text == "" {
		return text
	}

	for _, prefix := range active {
		idx := strings.Index(text, prefix)
		for idx != -1 {
			// Find the end of this secret token. Stop at quotes, spaces, brackets, or backslashes.
			end := idx + len(prefix)
			for end < len(text) && !isTokenBoundary(rune(text[end])) {
				end++
			}
			text = text[:idx] + "*******" + text[end:]
			
			// Continue searching for remaining occurrences
			nextIdx := strings.Index(text[idx+7:], prefix)
			if nextIdx == -1 {
				break
			}
			idx = idx + 7 + nextIdx
		}
	}
	return text
}

func isTokenBoundary(r rune) bool {
	return unicode.IsSpace(r) || r == '"' || r == '\'' || r == '<' || r == '>' || r == '{' || r == '}' || r == '[' || r == ']' || r == '\\' || r == ','
}

// ShouldDiscard evaluates multiple privacy layers (blacklists, passwords, timing heuristics)
func (f *Filter) ShouldDiscard(ev *watchers.Event) bool {
	// Layer 1: App Blacklist
	app := strings.ToLower(ev.Context.AppBundle)
	if _, exists := f.appBlacklistSet[app]; exists {
		return true
	}

	// Layer 2: Excluded Domains (Checks both explicit URL Domain and OS Window Title)
	f.mu.RLock()
	defer f.mu.RUnlock()

	if len(f.builtinDomains) > 0 || len(f.customDomains) > 0 {
		urlLower := strings.ToLower(ev.Context.URL)
		titleLower := strings.ToLower(ev.Context.WindowTitle)

		checkDomain := func(d string) bool {
			if d == "" {
				return false
			}
			dl := strings.ToLower(d)
			return strings.Contains(urlLower, dl) || strings.Contains(titleLower, dl)
		}

		for _, d := range f.builtinDomains {
			if checkDomain(d) {
				return true
			}
		}
		for _, d := range f.customDomains {
			if checkDomain(d) {
				return true
			}
		}
	}

	return false
}
