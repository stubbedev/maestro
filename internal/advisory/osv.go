package advisory

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util/fsstate"
	"github.com/stubbedev/maestro/internal/util/http"
)

const OSVEnv = "MAESTRO_OSV_ADVISORIES"

const (
	osvAPI             = "https://api.osv.dev/v1"
	osvEcosystem       = "Packagist"
	osvBatchSize       = 1000
	osvMaxPages        = 100
	osvSourceName      = "OSV"
	osvCacheMaxEntries = 1024
	osvCacheMaxAge     = 30 * 24 * time.Hour
)

var osvFormat = fsstate.Format{Name: "osv", Version: 1}

type OSV struct {
	http     http.Getter
	cacheDir string
}

func NewOSV(getter http.Getter, cacheDir string) *OSV {
	return &OSV{http: getter, cacheDir: cacheDir}
}

type osvRecord struct {
	ID               string        `json:"id"`
	Summary          string        `json:"summary,omitempty"`
	Details          string        `json:"details,omitempty"`
	Aliases          []string      `json:"aliases,omitempty"`
	Modified         string        `json:"modified"`
	Published        string        `json:"published,omitempty"`
	Withdrawn        string        `json:"withdrawn,omitempty"`
	Affected         []osvAffected `json:"affected,omitempty"`
	DatabaseSpecific struct {
		Severity string `json:"severity,omitempty"`
	} `json:"database_specific"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvAffected struct {
	Package  osvPackage `json:"package"`
	Ranges   []osvRange `json:"ranges,omitempty"`
	Versions []string   `json:"versions,omitempty"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
	Limit        string `json:"limit,omitempty"`
}

type osvPackageQuery struct {
	Package   osvPackage `json:"package"`
	PageToken string     `json:"page_token,omitempty"`
}

type osvListed struct {
	ID       string `json:"id"`
	Modified string `json:"modified"`
}

type osvBatchResult struct {
	Vulns         []osvListed `json:"vulns"`
	NextPageToken string      `json:"next_page_token"`
}

type osvQueryResult struct {
	Vulns         []osvRecord `json:"vulns"`
	NextPageToken string      `json:"next_page_token"`
}

func newOSVPackageQuery(name, pageToken string) osvPackageQuery {
	return osvPackageQuery{Package: osvPackage{Name: name, Ecosystem: osvEcosystem}, PageToken: pageToken}
}

func (o *OSV) Advisories(packageConstraintMap *repository.ConstraintMap, known *Advisories) (*Advisories, error) {
	listed, err := o.list(packageConstraintMap.Keys())
	if err != nil {
		return nil, err
	}

	parser := pkg.NewVersionParser()
	result := &Advisories{}
	for name, constraint := range packageConstraintMap.All() {
		vulns := listed[name]
		if len(vulns) == 0 {
			continue
		}
		records, err := o.records(name, vulns)
		if err != nil {
			return nil, err
		}
		knownForName, _ := known.Get(name)
		advisories, err := osvAdvisories(name, records, constraint, knownIDs(knownForName), parser)
		if err != nil {
			return nil, err
		}
		if len(advisories) > 0 {
			result.Set(name, advisories)
		}
	}

	return result, nil
}

func (o *OSV) list(names []string) (map[string][]osvListed, error) {
	type pending struct{ name, token string }
	queue := make([]pending, len(names))
	for i, name := range names {
		queue[i] = pending{name: name}
	}
	listed := make(map[string][]osvListed, len(names))
	pages := map[string]int{}
	for len(queue) > 0 {
		batch := queue[:min(osvBatchSize, len(queue))]
		queue = queue[len(batch):]
		queries := make([]osvPackageQuery, len(batch))
		for i, p := range batch {
			queries[i] = newOSVPackageQuery(p.name, p.token)
		}
		var answer struct {
			Results []osvBatchResult `json:"results"`
		}
		if err := o.post("/querybatch", map[string]any{"queries": queries}, &answer); err != nil {
			return nil, err
		}
		if len(answer.Results) != len(batch) {
			return nil, fmt.Errorf("osv.dev answered %d results to %d queries of %s/querybatch", len(answer.Results), len(batch), osvAPI)
		}
		for i, r := range answer.Results {
			name := batch[i].name
			listed[name] = append(listed[name], r.Vulns...)
			if r.NextPageToken == "" {
				continue
			}
			if pages[name]++; pages[name] >= osvMaxPages {
				return nil, fmt.Errorf("osv.dev lists more than %d pages of vulnerabilities of %s", osvMaxPages, name)
			}
			queue = append(queue, pending{name, r.NextPageToken})
		}
	}

	return listed, nil
}

func (o *OSV) records(name string, listed []osvListed) ([]osvRecord, error) {
	path := o.cachePath(name)
	if records, ok := loadOSVRecords(path, listed); ok {
		return records, nil
	}

	var records []osvRecord
	token := ""
	for page := 0; ; page++ {
		if page == osvMaxPages {
			return nil, fmt.Errorf("osv.dev has more than %d pages of vulnerabilities of %s", osvMaxPages, name)
		}
		var answer osvQueryResult
		if err := o.post("/query", newOSVPackageQuery(name, token), &answer); err != nil {
			return nil, err
		}
		records = append(records, answer.Vulns...)
		if token = answer.NextPageToken; token == "" {
			break
		}
	}
	slices.SortFunc(records, func(a, b osvRecord) int { return strings.Compare(a.ID, b.ID) })
	records = slices.CompactFunc(records, func(a, b osvRecord) bool { return a.ID == b.ID })
	storeOSVRecords(path, records)

	return records, nil
}

func (o *OSV) post(endpoint string, body, answer any) error {
	url := osvAPI + endpoint
	content, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding the request to %s: %w", url, err)
	}
	response, err := o.http.Get(url, filterlist.PostOptions(nil, "Content-type: application/json", string(content)))
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(response.Body()), answer); err != nil {
		return fmt.Errorf("decoding the answer of %s: %w", url, err)
	}

	return nil
}

func (o *OSV) cachePath(name string) string {
	if o.cacheDir == "" {
		return ""
	}
	k := fsstate.NewKeyHash()
	k.String(osvFormat.Header())
	k.String(name)

	return filepath.Join(o.cacheDir, hex.EncodeToString(k.Sum(nil)[:16]))
}

func loadOSVRecords(path string, listed []osvListed) ([]osvRecord, bool) {
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	body, ok := strings.CutPrefix(string(data), osvFormat.Header())
	if !ok {
		return nil, false
	}
	var records []osvRecord
	if json.Unmarshal([]byte(body), &records) != nil || len(records) != len(listed) {
		return nil, false
	}
	modified := make(map[string]string, len(records))
	for _, r := range records {
		modified[r.ID] = r.Modified
	}
	for _, l := range listed {
		if m, ok := modified[l.ID]; !ok || m != l.Modified {
			return nil, false
		}
	}

	return records, true
}

func storeOSVRecords(path string, records []osvRecord) {
	if path == "" {
		return
	}
	body, err := json.Marshal(records)
	if err != nil || fsstate.WriteAtomic(path, append([]byte(osvFormat.Header()), body...)) != nil {
		return
	}
	fsstate.Prune(filepath.Dir(path), osvCacheMaxEntries, osvCacheMaxAge)
}

func knownIDs(advisories []Advisory) map[string]bool {
	ids := map[string]bool{}
	for _, a := range advisories {
		ids[a.Partial().AdvisoryID] = true
		security, ok := AsSecurityAdvisory(a)
		if !ok {
			continue
		}
		if security.CVE.Valid {
			ids[security.CVE.S] = true
		}
		for _, source := range sources(security) {
			if s, ok := source.(*php.Array); ok {
				if remoteID, ok := s.Get("remoteId"); ok {
					ids[php.ToString(remoteID)] = true
				}
			}
		}
	}

	return ids
}

func osvAdvisories(name string, records []osvRecord, constraint semver.ConstraintInterface, known map[string]bool, parser *pkg.VersionParser) ([]Advisory, error) {
	var advisories []Advisory
	for _, r := range records {
		if r.Withdrawn != "" || known[r.ID] || slices.ContainsFunc(r.Aliases, func(alias string) bool { return known[alias] }) {
			continue
		}
		affected := osvAffectedVersions(name, r.Affected, parser)
		if affected == "" {
			continue
		}
		advisory, err := repository.CreatePartialSecurityAdvisory(name, osvAdvisoryData(name, r, affected), parser)
		if err != nil {
			return nil, fmt.Errorf("reading OSV advisory %s of %s: %w", r.ID, name, err)
		}
		if advisory.Partial().AffectedVersions.Matches(constraint) {
			advisories = append(advisories, advisory)
		}
	}

	return advisories, nil
}

func osvAdvisoryData(name string, r osvRecord, affected string) *php.Array {
	reportedAt := r.Published
	if reportedAt == "" {
		reportedAt = r.Modified
	}

	return php.ArrayOf(
		"advisoryId", r.ID,
		"packageName", name,
		"affectedVersions", affected,
		"title", osvTitle(r),
		"sources", php.ListOf(php.ArrayOf("name", osvSourceName, "remoteId", r.ID)),
		"reportedAt", reportedAt,
		"cve", osvCVE(r),
		"link", "https://osv.dev/vulnerability/"+r.ID,
		"severity", osvSeverity(r.DatabaseSpecific.Severity),
	)
}

func osvTitle(r osvRecord) string {
	if r.Summary != "" {
		return r.Summary
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(r.Details), "\n"); line != "" {
		return line
	}

	return r.ID
}

func osvCVE(r osvRecord) any {
	if strings.HasPrefix(r.ID, "CVE-") {
		return r.ID
	}
	for _, id := range r.Aliases {
		if strings.HasPrefix(id, "CVE-") {
			return id
		}
	}

	return nil
}

func osvSeverity(severity string) any {
	switch s := php.Strtolower(severity); s {
	case "":
		return nil
	case "moderate":
		return "medium"
	default:
		return s
	}
}

func osvAffectedVersions(name string, affected []osvAffected, parser *pkg.VersionParser) string {
	var alternatives []string
	for _, a := range affected {
		if a.Package.Ecosystem != osvEcosystem || php.Strcasecmp(a.Package.Name, name) != 0 {
			continue
		}
		var intervals []string
		for _, r := range a.Ranges {
			if r.Type == "ECOSYSTEM" || r.Type == "SEMVER" {
				intervals = append(intervals, osvRangeIntervals(r.Events, parser)...)
			}
		}
		if len(intervals) == 0 {
			for _, v := range a.Versions {
				if _, err := parser.Normalize(v); err == nil {
					intervals = append(intervals, v)
				}
			}
		}
		alternatives = append(alternatives, intervals...)
	}

	return strings.Join(alternatives, " || ")
}

type osvBound uint8

const (
	osvIntroduced osvBound = iota
	osvBelow
	osvUpTo
)

type osvPoint struct {
	version, normalized string
	bound               osvBound
}

func osvPointOf(e osvEvent) (osvPoint, bool) {
	switch {
	case e.Introduced != "":
		return osvPoint{version: e.Introduced, bound: osvIntroduced}, true
	case e.Fixed != "":
		return osvPoint{version: e.Fixed, bound: osvBelow}, true
	case e.LastAffected != "":
		return osvPoint{version: e.LastAffected, bound: osvUpTo}, true
	case e.Limit != "":
		return osvPoint{version: e.Limit, bound: osvBelow}, true
	}

	return osvPoint{}, false
}

func osvRangeIntervals(events []osvEvent, parser *pkg.VersionParser) []string {
	points := make([]osvPoint, 0, len(events))
	for _, e := range events {
		p, ok := osvPointOf(e)
		if !ok {
			continue
		}
		if p.bound != osvIntroduced || p.version != "0" {
			normalized, err := parser.Normalize(p.version)
			if err != nil {
				return nil
			}
			p.normalized = normalized
		}
		points = append(points, p)
	}
	slices.SortStableFunc(points, func(a, b osvPoint) int {
		switch {
		case a.normalized == b.normalized:
			return 0
		case a.normalized == "":
			return -1
		case b.normalized == "":
			return 1
		}

		return semver.VersionCompare(a.normalized, b.normalized)
	})

	var intervals []string
	open, lower := false, ""
	for _, p := range points {
		switch {
		case p.bound == osvIntroduced && !open:
			open, lower = true, ""
			if p.normalized != "" {
				lower = ">=" + p.version + " "
			}
		case p.bound == osvBelow && open:
			open = false
			intervals = append(intervals, lower+"<"+p.version)
		case p.bound == osvUpTo && open:
			open = false
			intervals = append(intervals, lower+"<="+p.version)
		}
	}
	if open {
		intervals = append(intervals, cmp.Or(strings.TrimSpace(lower), "*"))
	}

	return intervals
}
