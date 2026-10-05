// Ports src/Composer/Util/Perforce.php.

package vcs

import (
	"errors"
	stdio "io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// phpEOL is PHP_EOL.
var phpEOL = func() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}

	return "\n"
}()

// symfonyProcessTimeout is the default timeout of a Symfony Process.
const symfonyProcessTimeout = 60 * time.Second

// p4Executable is Perforce::getP4Executable's `static $p4Executable`.
var p4Executable = sync.OnceValue(func() string {
	if path, ok := util.NewExecutableFinder().Find("p4"); ok {
		return path
	}

	return "p4"
})

// PerforceFilesystem is the part of Composer\Util\Filesystem Perforce
// uses. *util.Filesystem implements it.
type PerforceFilesystem interface {
	Remove(file string) (bool, error)
}

// Perforce ports Composer\Util\Perforce.
type Perforce struct {
	path          string
	p4Depot       string
	p4Client      string // "" until getClient() computed it
	p4User        *string
	p4Password    *string
	p4Port        string
	p4Stream      *string
	p4DepotType   string
	p4Branch      string
	process       Process
	uniqueName    string
	windowsFlag   bool
	commandResult string
	io            io.IO
	filesystem    PerforceFilesystem
	// p4Executable overrides the p4 found in PATH (tests).
	p4Executable string
}

// NewPerforce is new Perforce($repoConfig, $port, $path, $process,
// $isWindows, $io). repoConfig holds unique_perforce_client_name, depot,
// branch, p4user and p4password, all optional; it may be nil.
func NewPerforce(repoConfig *php.Array, port, path string, process Process, isWindows bool, ioi io.IO) (*Perforce, error) {
	return newPerforce(repoConfig, port, path, process, isWindows, ioi, "")
}

func newPerforce(repoConfig *php.Array, port, path string, process Process, isWindows bool, ioi io.IO, executable string) (*Perforce, error) {
	if !IsValidPort(port) {
		return nil, &util.SecurityError{Message: "Invalid Perforce port (" + port + "), it must be of the form [tcp|ssl:][host:]port"}
	}

	p := &Perforce{windowsFlag: isWindows, p4Port: port, process: process, p4Executable: executable}

	if err := p.InitializePath(path); err != nil {
		return nil, err
	}

	if err := p.Initialize(repoConfig); err != nil {
		return nil, err
	}

	p.io = ioi

	return p, nil
}

// CreatePerforce ports Perforce::create.
func CreatePerforce(repoConfig *php.Array, port, path string, process Process, ioi io.IO) (*Perforce, error) {
	return NewPerforce(repoConfig, port, path, process, util.IsWindows(), ioi)
}

// CheckServerExists ports Perforce::checkServerExists. An unusable port is
// not a perforce server rather than an error, as this probe runs against
// every VCS repository url no driver matched yet.
func CheckServerExists(url string, processExecutor Process) (bool, error) {
	if !IsValidPort(url) {
		return false, nil
	}

	var ignoredOutput string

	code, err := processExecutor.Execute(util.Cmd("p4", "-p", url, "info", "-s"), &ignoredOutput, "")

	return err == nil && code == 0, err
}

var (
	p4CommandTransport = php.MustCompile(`{^\s*+(?:rsh|jsh)\s*+:}i`)
	p4PortShape        = php.MustCompile(`{^(?:(?:tcp|ssl)(?:4|6|46|64)?:)?(?:\[[0-9a-f:.]++\]|[a-z0-9._][a-z0-9._-]*+)(?::[a-z0-9._][a-z0-9._-]*+)?$}iD`)
)

// IsValidPort ports Perforce::isValidPort: whether url is a P4PORT
// network endpoint ([transport:][host:]port). An rsh:/jsh: P4PORT makes
// the p4 client run the rest of the value as a local command
// (GHSA-rvx4-ffvw-m9q3).
func IsValidPort(url string) bool {
	// rsh/jsh are transport keywords to p4, so "rsh:foo" never parses as host "rsh" port "foo"
	// and has to be rejected before the shape check below would happily accept it
	if ok, _ := p4CommandTransport.IsMatch(url); ok {
		return false
	}

	ok, _ := p4PortShape.IsMatch(url)

	return ok
}

// configString is isset($repoConfig[$key]) and its value.
func configString(repoConfig *php.Array, key string) (string, bool) {
	if repoConfig == nil {
		return "", false
	}

	v, ok := repoConfig.Get(key)
	if !ok || v == nil {
		return "", false
	}

	return php.ToString(v), true
}

// Initialize ports initialize($repoConfig).
func (p *Perforce) Initialize(repoConfig *php.Array) error {
	p.uniqueName = p.GenerateUniquePerforceClientName()
	if repoConfig == nil || repoConfig.Len() == 0 {
		return nil
	}

	if v, ok := configString(repoConfig, "unique_perforce_client_name"); ok {
		p.uniqueName = v
	}

	if v, ok := configString(repoConfig, "depot"); ok {
		p.p4Depot = v
	}

	if v, ok := configString(repoConfig, "branch"); ok {
		p.p4Branch = v
	}

	if v, ok := configString(repoConfig, "p4user"); ok {
		p.p4User = &v
	} else {
		user, err := p.getP4variable("P4USER")
		if err != nil {
			return err
		}

		p.p4User = user
	}

	if v, ok := configString(repoConfig, "p4password"); ok {
		p.p4Password = &v
	}

	return nil
}

// InitializeDepotAndBranch ports initializeDepotAndBranch(); nil leaves a
// value unchanged.
func (p *Perforce) InitializeDepotAndBranch(depot, branch *string) {
	if depot != nil {
		p.p4Depot = *depot
	}

	if branch != nil {
		p.p4Branch = *branch
	}
}

// GenerateUniquePerforceClientName ports
// generateUniquePerforceClientName(): hostname_time.
func (p *Perforce) GenerateUniquePerforceClientName() string {
	hostname, _ := os.Hostname()

	return hostname + "_" + strconv.FormatInt(time.Now().Unix(), 10)
}

// CleanupClientSpec ports cleanupClientSpec(): deletes the client and its
// spec file.
func (p *Perforce) CleanupClientSpec() error {
	client := p.GetClient()
	task := []string{"client", "-d", client}
	useP4Client := false
	command := p.GenerateP4Command(task, useP4Client)

	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return err
	}

	clientSpec := p.GetP4ClientSpec()
	_, err := p.GetFilesystem().Remove(clientSpec)

	return err
}

// executeCommand runs command, its output going to commandResult. cwd
// replaces syncCodeBase's chdir(), which would change the directory of the
// whole process.
func (p *Perforce) executeCommand(command util.Command, cwd string) (int, error) {
	p.commandResult = ""

	return p.process.Execute(command, &p.commandResult, cwd)
}

// GetClient ports getClient().
func (p *Perforce) GetClient() string {
	if p.p4Client == "" {
		cleanStreamName := strings.NewReplacer("//", "", "/", "_", "@", "").Replace(p.GetStream())
		p.p4Client = "composer_perforce_" + p.uniqueName + "_" + cleanStreamName
	}

	return p.p4Client
}

// InitializePath ports initializePath(): sets the path, creating it.
func (p *Perforce) InitializePath(path string) error {
	p.path = path

	return util.EnsureDirectoryExists(path)
}

// SetStream ports setStream().
func (p *Perforce) SetStream(stream string) {
	p.p4Stream = &stream
	// Stream format is //depot/stream, while non-streaming depot is //depot
	if index := strings.LastIndex(stream, "/"); index > 2 {
		p.p4DepotType = "stream"
	}
}

// IsStream ports isStream().
func (p *Perforce) IsStream() bool {
	return p.p4DepotType == "stream"
}

// GetStream ports getStream().
func (p *Perforce) GetStream() string {
	if p.p4Stream == nil {
		var stream string
		if p.IsStream() {
			stream = "//" + p.p4Depot + "/" + p.p4Branch
		} else {
			stream = "//" + p.p4Depot
		}

		p.p4Stream = &stream
	}

	return *p.p4Stream
}

// GetStreamWithoutLabel ports getStreamWithoutLabel().
func (p *Perforce) GetStreamWithoutLabel(stream string) string {
	before, _, _ := strings.Cut(stream, "@")

	return before
}

// GetP4ClientSpec ports getP4ClientSpec(): the path of the client spec
// file.
func (p *Perforce) GetP4ClientSpec() string {
	return p.path + "/" + p.GetClient() + ".p4.spec"
}

// GetUser ports getUser(); nil for null.
func (p *Perforce) GetUser() *string { return p.p4User }

// SetUser ports setUser().
func (p *Perforce) SetUser(user *string) { p.p4User = user }

// QueryP4User ports queryP4User(): the user from P4USER, or asked for.
func (p *Perforce) QueryP4User() error {
	if len(strOf(p.p4User)) > 0 {
		return nil
	}

	user, err := p.getP4variable("P4USER")
	if err != nil {
		return err
	}

	p.p4User = user
	if len(strOf(p.p4User)) > 0 {
		return nil
	}

	answer, err := p.io.Ask("Enter P4 User:", nil)
	if err != nil {
		return err
	}

	p.p4User = nil

	if answer != nil {
		s := php.ToString(answer)
		p.p4User = &s
	}

	var command string
	if p.windowsFlag {
		command = p.getP4Executable() + " set P4USER=" + util.Escape(strOf(p.p4User))
	} else {
		command = "export P4USER=" + util.Escape(strOf(p.p4User))
	}

	_, err = p.executeCommand(util.ShellCmd(command), "")

	return err
}

// getP4variable reads a p4 variable: from `p4 set` on Windows, the
// environment otherwise. nil is null.
func (p *Perforce) getP4variable(name string) (*string, error) {
	if p.windowsFlag {
		command := p.getP4Executable() + " set"
		if _, err := p.executeCommand(util.ShellCmd(command), ""); err != nil {
			return nil, err
		}

		result := php.Trim(p.commandResult)
		for line := range strings.SplitSeq(result, phpEOL) {
			fields := strings.Split(line, "=")
			if fields[0] != name {
				continue
			}

			value := field(fields, 1)
			if index := strings.Index(value, " "); index >= 0 {
				value = value[:index]
			}

			value = php.Trim(value)

			return &value, nil
		}

		return nil, nil
	}

	command := "echo $" + name
	if _, err := p.executeCommand(util.ShellCmd(command), ""); err != nil {
		return nil, err
	}

	result := php.Trim(p.commandResult)

	return &result, nil
}

// field is $fields[$i], "" (null) when it does not exist.
func field(fields []string, i int) string {
	if i < len(fields) {
		return fields[i]
	}

	return ""
}

// QueryP4Password ports queryP4Password(): the password from the repo
// config, P4PASSWD, or asked for. nil is null.
func (p *Perforce) QueryP4Password() (*string, error) {
	if p.p4Password != nil {
		return p.p4Password, nil
	}

	password, err := p.getP4variable("P4PASSWD")
	if err != nil {
		return nil, err
	}

	if len(strOf(password)) <= 0 {
		answer, err := p.io.AskAndHideAnswer("Enter password for Perforce user " + strOf(p.GetUser()) + ": ")
		if err != nil {
			return nil, err
		}

		password = nil

		if answer != nil {
			s := php.ToString(answer)
			password = &s
		}
	}

	p.p4Password = password

	return password, nil
}

// GenerateP4Command ports generateP4Command(): p4 with the user, client
// (when useClient) and port options, then arguments.
func (p *Perforce) GenerateP4Command(arguments []string, useClient bool) []string {
	p4Command := make([]string, 0, 7+len(arguments))
	p4Command = append(p4Command, p.getP4Executable())

	if p.GetUser() != nil {
		p4Command = append(p4Command, "-u", *p.GetUser())
	}

	if useClient {
		p4Command = append(p4Command, "-c", p.GetClient())
	}

	p4Command = append(p4Command, "-p", p.p4Port)

	return append(p4Command, arguments...)
}

// IsLoggedIn ports isLoggedIn().
func (p *Perforce) IsLoggedIn() (bool, error) {
	command := p.GenerateP4Command([]string{"login", "-s"}, false)

	exitCode, err := p.executeCommand(util.Cmd(command...), "")
	if err != nil {
		return false, err
	}

	if exitCode != 0 {
		errorOutput := p.process.GetErrorOutput()
		if !strings.Contains(errorOutput, strOf(p.GetUser())) {
			if !strings.Contains(errorOutput, "p4") {
				return false, nil
			}

			return false, errors.New("p4 command not found in path: " + errorOutput)
		}

		return false, errors.New("Invalid user name: " + strOf(p.GetUser()))
	}

	return true, nil
}

// ConnectClient ports connectClient(): creates the client from its spec
// file.
func (p *Perforce) ConnectClient() error {
	p4CreateClientCommand := p.GenerateP4Command([]string{"client", "-i"}, true)

	spec, err := os.ReadFile(p.GetP4ClientSpec())
	if err != nil {
		return &util.ErrorException{Message: "file_get_contents(" + p.GetP4ClientSpec() + "): Failed to open stream: " + util.Strerror(err)}
	}

	process := util.NewProcess(p4CreateClientCommand, "", nil, symfonyProcessTimeout)
	process.SetInput(string(spec))
	_, err = process.Run(nil)

	return err
}

// SyncCodeBase ports syncCodeBase(): p4 sync -f in the path, at
// sourceReference unless it is nil.
func (p *Perforce) SyncCodeBase(sourceReference *string) error {
	p4SyncCommand := p.GenerateP4Command([]string{"sync", "-f"}, true)
	if sourceReference != nil {
		p4SyncCommand = append(p4SyncCommand, "@"+*sourceReference)
	}

	_, err := p.executeCommand(util.Cmd(p4SyncCommand...), p.path)

	return err
}

// WriteClientSpecToFile ports writeClientSpecToFile().
func (p *Perforce) WriteClientSpecToFile(spec stdio.Writer) error {
	now := time.Now().Format("2006/01/02 15:04:05")
	user := strOf(p.GetUser())

	var b strings.Builder

	b.WriteString("Client: " + p.GetClient() + phpEOL + phpEOL)
	b.WriteString("Update: " + now + phpEOL + phpEOL)
	b.WriteString("Access: " + now + phpEOL)
	b.WriteString("Owner:  " + user + phpEOL + phpEOL)
	b.WriteString("Description:" + phpEOL)
	b.WriteString("  Created by " + user + " from composer." + phpEOL + phpEOL)
	b.WriteString("Root: " + p.path + phpEOL + phpEOL)
	b.WriteString("Options:  noallwrite noclobber nocompress unlocked modtime rmdir" + phpEOL + phpEOL)
	b.WriteString("SubmitOptions:  revertunchanged" + phpEOL + phpEOL)
	b.WriteString("LineEnd:  local" + phpEOL + phpEOL)

	if p.IsStream() {
		b.WriteString("Stream:" + phpEOL)
		b.WriteString("  " + p.GetStreamWithoutLabel(strOf(p.p4Stream)) + phpEOL)
	} else {
		b.WriteString("View:  " + p.GetStream() + "/...  //" + p.GetClient() + "/... " + phpEOL)
	}

	_, err := stdio.WriteString(spec, b.String())

	return err
}

// WriteP4ClientSpec ports writeP4ClientSpec().
func (p *Perforce) WriteP4ClientSpec() error {
	clientSpec := p.GetP4ClientSpec()

	spec, err := os.Create(clientSpec)
	if err != nil {
		return &util.ErrorException{Message: "fopen(" + clientSpec + "): Failed to open stream: " + util.Strerror(err)}
	}

	if err := p.WriteClientSpecToFile(spec); err != nil {
		_ = spec.Close()

		return err
	}

	return spec.Close()
}

// WindowsLogin ports windowsLogin(): p4 login -a with password as input
// (nil for none).
func (p *Perforce) WindowsLogin(password *string) (int, error) {
	command := p.GenerateP4Command([]string{"login", "-a"}, true)

	process := util.NewProcess(command, "", nil, symfonyProcessTimeout)
	if password != nil {
		process.SetInput(*password)
	}

	return process.Run(nil)
}

// P4Login ports p4Login(): logs in unless already logged in.
func (p *Perforce) P4Login() error {
	if err := p.QueryP4User(); err != nil {
		return err
	}

	loggedIn, err := p.IsLoggedIn()
	if err != nil || loggedIn {
		return err
	}

	password, err := p.QueryP4Password()
	if err != nil {
		return err
	}

	if p.windowsFlag {
		_, err := p.WindowsLogin(password)

		return err
	}

	command := p.GenerateP4Command([]string{"login", "-a"}, false)

	process := util.NewProcess(command, "", nil, symfonyProcessTimeout)
	if password != nil {
		process.SetInput(*password)
	}

	if _, err := process.Run(nil); err != nil {
		return err
	}

	if !process.IsSuccessful() {
		return errors.New("Error logging in:" + p.process.GetErrorOutput())
	}

	return nil
}

// GetComposerInformation ports getComposerInformation(): the decoded
// composer.json at identifier, nil for null.
func (p *Perforce) GetComposerInformation(identifier string) (*php.Array, error) {
	composerFileContent, ok, err := p.GetFileContent("composer.json", identifier)
	if err != nil || !ok || !php.ToBool(composerFileContent) {
		return nil, err
	}

	decoded, err := php.JSONDecode(composerFileContent, true)
	if err != nil {
		return nil, nil //nolint:nilerr // json_decode() returns null on invalid JSON
	}

	a, _ := decoded.(*php.Array)

	return a, nil
}

// GetFileContent ports getFileContent(); false for null.
func (p *Perforce) GetFileContent(file, identifier string) (string, bool, error) {
	path, ok, err := p.GetFilePath(file, identifier)
	if err != nil || !ok {
		return "", false, err
	}

	command := p.GenerateP4Command([]string{"print", path}, true)
	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return "", false, err
	}

	result := p.commandResult

	if !php.ToBool(php.Trim(result)) {
		return "", false, nil
	}

	return result, true, nil
}

// GetFilePath ports getFilePath(); false for null.
func (p *Perforce) GetFilePath(file, identifier string) (string, bool, error) {
	index := strings.Index(identifier, "@")
	if index < 0 {
		return identifier + "/" + file, true, nil
	}

	path := identifier[:index] + "/" + file + identifier[index:]
	command := p.GenerateP4Command([]string{"files", path}, false)

	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return "", false, err
	}

	result := p.commandResult
	if !strings.Contains(result, "no such file(s).") {
		if index3 := strings.Index(result, "change"); index3 >= 0 {
			phrase := php.Trim(result[index3:])
			fields := strings.Split(phrase, " ")

			return identifier[:index] + "/" + file + "@" + field(fields, 1), true, nil
		}
	}

	return "", false, nil
}

// GetBranches ports getBranches(): ['master' => stream@lastchange].
func (p *Perforce) GetBranches() (*php.Array, error) {
	possibleBranches := php.NewArray()

	if !p.IsStream() {
		possibleBranches.Set(p.p4Branch, p.GetStream())
	} else {
		command := p.GenerateP4Command([]string{"streams", "//" + p.p4Depot + "/..."}, true)
		if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
			return nil, err
		}

		for line := range strings.SplitSeq(p.commandResult, phpEOL) {
			resBits := strings.Split(line, " ")
			if len(resBits) > 4 {
				branch, _, err := php.PregReplace(`/[^A-Za-z0-9 ]/`, "", resBits[4], -1)
				if err != nil {
					return nil, err
				}

				possibleBranches.Set(branch, resBits[1])
			}
		}
	}

	command := p.GenerateP4Command([]string{"changes", p.GetStream() + "/..."}, false)
	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return nil, err
	}

	lastCommit, _, _ := strings.Cut(p.commandResult, phpEOL)
	lastCommitNum := field(strings.Split(lastCommit, " "), 1)

	branch, _ := possibleBranches.Get(p.p4Branch)

	return php.ArrayOf("master", php.ToString(branch)+"@"+lastCommitNum), nil
}

// GetTags ports getTags(): label => stream@label.
func (p *Perforce) GetTags() (*php.Array, error) {
	command := p.GenerateP4Command([]string{"labels"}, true)
	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return nil, err
	}

	tags := php.NewArray()

	for line := range strings.SplitSeq(p.commandResult, phpEOL) {
		if strings.Contains(line, "Label") {
			fields := strings.Split(line, " ")
			tags.Set(field(fields, 1), p.GetStream()+"@"+field(fields, 1))
		}
	}

	return tags, nil
}

// CheckStream ports checkStream(): whether the depot is a stream depot.
func (p *Perforce) CheckStream() (bool, error) {
	command := p.GenerateP4Command([]string{"depots"}, false)
	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return false, err
	}

	for line := range strings.SplitSeq(p.commandResult, phpEOL) {
		if strings.Contains(line, "Depot") {
			fields := strings.Split(line, " ")
			if p.p4Depot == field(fields, 1) {
				p.p4DepotType = field(fields, 3)

				return p.IsStream(), nil
			}
		}
	}

	return false, nil
}

// getChangeList returns the change number of reference's label; false for
// null.
func (p *Perforce) getChangeList(reference string) (string, bool, error) {
	index := strings.Index(reference, "@")
	if index < 0 {
		return "", false, nil
	}

	label := reference[index:]
	command := p.GenerateP4Command([]string{"changes", "-m1", label}, true)

	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return "", false, err
	}

	changes := p.commandResult
	if !strings.HasPrefix(changes, "Change") {
		return "", false, nil
	}

	return field(strings.Split(changes, " "), 1), true, nil
}

// GetCommitLogs ports getCommitLogs(); false for null.
func (p *Perforce) GetCommitLogs(fromReference, toReference string) (string, bool, error) {
	fromChangeList, ok, err := p.getChangeList(fromReference)
	if err != nil || !ok {
		return "", false, err
	}

	toChangeList, ok, err := p.getChangeList(toReference)
	if err != nil || !ok {
		return "", false, err
	}

	index := strings.Index(fromReference, "@")
	main := fromReference[:index] + "/..."
	command := p.GenerateP4Command([]string{"filelog", main + "@" + fromChangeList + "," + toChangeList}, true)

	if _, err := p.executeCommand(util.Cmd(command...), ""); err != nil {
		return "", false, err
	}

	return p.commandResult, true, nil
}

// GetFilesystem ports getFilesystem().
func (p *Perforce) GetFilesystem() PerforceFilesystem {
	if p.filesystem == nil {
		p.filesystem = util.NewFilesystem(asExecutor(p.process))
	}

	return p.filesystem
}

// SetFilesystem ports setFilesystem().
func (p *Perforce) SetFilesystem(fs PerforceFilesystem) { p.filesystem = fs }

func (p *Perforce) getP4Executable() string {
	if p.p4Executable != "" {
		return p.p4Executable
	}

	return p4Executable()
}
