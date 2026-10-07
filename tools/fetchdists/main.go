// Command fetchdists downloads the dist archives of popular Composer
// packages for the store's differential test (internal/store,
// TestDifferentialRealDists), which runs over them when they are present
// and skips otherwise. Packages on GitHub are fetched as zipball and as
// tarball, to cover ZipDownloader and TarDownloader.
//
//	go run ./tools/fetchdists [-dir DIR]
//
// DIR defaults to $MAESTRO_TEST_DISTS, else <user cache dir>/maestro-test-dists.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/switches"
)

var packages = []string{
	"composer/composer",
	"doctrine/orm",
	"egulias/email-validator",
	"friendsofphp/php-cs-fixer",
	"guzzlehttp/guzzle",
	"laravel/framework",
	"league/flysystem",
	"monolog/monolog",
	"myclabs/deep-copy",
	"nesbot/carbon",
	"nikic/php-parser",
	"phpstan/phpstan",
	"phpunit/phpunit",
	"predis/predis",
	"psr/log",
	"ramsey/uuid",
	"sebastian/diff",
	"symfony/console",
	"symfony/polyfill-mbstring",
	"twig/twig",
}

type dist struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	Reference string `json:"reference"`
}

func main() {
	def := os.Getenv(switches.TestDists)
	if def == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			def = filepath.Join(dir, "maestro-test-dists")
		}
	}

	dir := flag.String("dir", def, "where to store the archives")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fail(err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	failed := false

	for _, name := range packages {
		if err := fetchPackage(client, *dir, name); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// fetchPackage downloads the latest release's dist, and its tarball when
// the dist is a GitHub zipball.
func fetchPackage(client *http.Client, dir, name string) error {
	var meta struct {
		Packages map[string][]struct {
			Version string `json:"version"`
			Dist    dist   `json:"dist"`
		} `json:"packages"`
	}

	if err := getJSON(client, "https://repo.packagist.org/p2/"+name+".json", &meta); err != nil {
		return err
	}

	versions := meta.Packages[name]
	if len(versions) == 0 || versions[0].Dist.URL == "" {
		return errors.New("no dist")
	}

	d := versions[0].Dist
	base := strings.ReplaceAll(name, "/", "--") + "-" + versions[0].Version

	if err := download(client, d.URL, filepath.Join(dir, base+"."+d.Type)); err != nil {
		return err
	}

	if strings.Contains(d.URL, "/zipball/") {
		return download(client, strings.Replace(d.URL, "/zipball/", "/tarball/", 1), filepath.Join(dir, base+".tar.gz"))
	}

	return nil
}

func getJSON(client *http.Client, url string, v any) error {
	body, err := get(client, url)
	if err != nil {
		return err
	}

	defer func() { _ = body.Close() }()

	return json.NewDecoder(body).Decode(v)
}

func get(client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "maestro-fetchdists")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}

	return resp.Body, nil
}

// download saves url at path unless path exists, atomically.
func download(client *http.Client, url, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	body, err := get(client, url)
	if err != nil {
		return err
	}

	defer func() { _ = body.Close() }()

	tmp := path + ".part"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	fmt.Println(path)

	return os.Rename(tmp, path)
}
