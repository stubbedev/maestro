// Composer\Util\Filesystem, ProcessExecutor and Json\JsonFile
// (docs/PLUGINS.md §4.8, §4.10): `fs.*`, `proc.*` and `json.*`, run by
// maestro's ports.

package plugin

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/util"
)

// commandArg is a $command param: a string (run through the shell) or a
// list of arguments.
func commandArg(v any) util.Command {
	if list, ok := v.(*php.Array); ok {
		argv := make([]string, 0, list.Len())
		for _, v := range list.Values() {
			argv = append(argv, php.ToString(v))
		}

		return util.Cmd(argv...)
	}

	return util.ShellCmd(php.ToString(v))
}

func (r *Runtime) registerUtil() {
	r.registerManipulator()
	r.registerAsyncProcesses()
	r.registerConfigSources()
	fs := func(name string, fn func(a args) (any, error)) {
		r.Handle("fs."+name, func(v any) (any, error) { return fn(argsOf("fs."+name, v)) })
	}
	filesystem := func() *util.Filesystem { return util.NewFilesystem(util.NewProcessExecutor(nil)) }

	fs("isDirEmpty", func(a args) (any, error) { return util.IsDirEmpty(a.str(0)) })
	fs("emptyDirectory", func(a args) (any, error) {
		ensure := true
		if a.has(1) {
			ensure = a.boolean(1)
		}

		return nil, filesystem().EmptyDirectory(a.str(0), ensure)
	})
	fs("removeDirectory", func(a args) (any, error) { return filesystem().RemoveDirectory(a.str(0)) })
	fs("removeDirectoryPhp", func(a args) (any, error) { return util.RemoveDirectoryPhp(a.str(0)) })
	// Filesystem::removeEdgeCases() (private) for removeDirectoryAsync():
	// whether the removal was done, or null.
	fs("removeEdgeCases", func(a args) (any, error) {
		result, done, err := util.RemoveEdgeCases(a.str(0))
		if err != nil || !done {
			return nil, err
		}

		return result, nil
	})
	fs("ensureDirectoryExists", func(a args) (any, error) { return nil, util.EnsureDirectoryExists(a.str(0)) })
	fs("copyThenRemove", func(a args) (any, error) { return nil, util.CopyThenRemove(a.str(0), a.str(1)) })
	fs("copy", func(a args) (any, error) { return util.Copy(a.str(0), a.str(1)) })
	fs("rename", func(a args) (any, error) { return nil, filesystem().Rename(a.str(0), a.str(1)) })
	fs("findShortestPath", func(a args) (any, error) {
		return util.FindShortestPath(a.str(0), a.str(1), a.boolean(2), a.boolean(3))
	})
	fs("findShortestPathCode", func(a args) (any, error) {
		return util.FindShortestPathCode(a.str(0), a.str(1), a.boolean(2), a.boolean(3), a.boolean(4))
	})
	fs("isAbsolutePath", func(a args) (any, error) { return util.IsAbsolutePath(a.str(0)), nil })
	fs("size", func(a args) (any, error) { return util.Size(a.str(0)) })
	fs("normalizePath", func(a args) (any, error) { return util.NormalizePath(a.str(0)), nil })
	fs("trimTrailingSlash", func(a args) (any, error) { return util.TrimTrailingSlash(a.str(0)), nil })
	fs("isLocalPath", func(a args) (any, error) { return util.IsLocalPath(a.str(0)), nil })
	fs("getPlatformPath", func(a args) (any, error) { return util.GetPlatformPath(a.str(0)), nil })
	fs("isReadable", func(a args) (any, error) { return util.IsReadable(a.str(0)), nil })
	fs("relativeSymlink", func(a args) (any, error) { return util.RelativeSymlink(a.str(0), a.str(1)) })
	fs("isSymlinkedDirectory", func(a args) (any, error) { return util.IsSymlinkedDirectory(a.str(0)), nil })
	fs("junction", func(a args) (any, error) { return nil, filesystem().Junction(a.str(0), a.str(1)) })
	fs("isJunction", func(a args) (any, error) { return util.IsJunction(a.str(0)), nil })
	fs("removeJunction", func(a args) (any, error) { return util.RemoveJunction(a.str(0)) })
	fs("filePutContentsIfModified", func(a args) (any, error) {
		n, err := util.FilePutContentsIfModified(a.str(0), []byte(a.str(1)))
		if err != nil {
			return nil, err
		}

		return int64(n), nil
	})
	fs("safeCopy", func(a args) (any, error) { return nil, util.SafeCopy(a.str(0), a.str(1)) })
	fs("remove", func(a args) (any, error) { return filesystem().Remove(a.str(0)) })
	fs("unlink", func(a args) (any, error) {
		if err := util.Unlink(a.str(0)); err != nil {
			return nil, err
		}

		return true, nil
	})
	fs("rmdir", func(a args) (any, error) {
		if err := util.Rmdir(a.str(0)); err != nil {
			return nil, err
		}

		return true, nil
	})

	// proc.execute: command, cwd, io, capture, tty.
	r.Handle("proc.execute", func(v any) (any, error) {
		a := argsOf("proc.execute", v)
		out, ok, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		var pio util.IO
		if ok {
			pio = out
		}
		pe := util.NewProcessExecutor(pio)
		cwd, _ := a.nullableString(1)
		var (
			code   int
			output string
		)
		switch {
		case a.has(5):
			code, err = r.executeWithCallback(pe, commandArg(a.at(0)), cwd, a.at(5))
		case a.boolean(4):
			code, err = pe.ExecuteTty(commandArg(a.at(0)), cwd)
		case a.boolean(3):
			code, err = pe.Execute(commandArg(a.at(0)), &output, cwd)
		default:
			code, err = pe.Execute(commandArg(a.at(0)), nil, cwd)
		}
		if err != nil {
			return nil, err
		}

		return php.ArrayOf("code", int64(code), "output", output, "errorOutput", pe.GetErrorOutput()), nil
	})
	r.Handle("proc.splitLines", func(v any) (any, error) {
		a := argsOf("proc.splitLines", v)

		return php.StringList(util.SplitLines(a.str(0))), nil
	})
	r.Handle("proc.escape", func(v any) (any, error) {
		a := argsOf("proc.escape", v)

		return util.Escape(a.str(0)), nil
	})
	r.Handle("proc.requiresGitDirEnv", func(v any) (any, error) {
		a := argsOf("proc.requiresGitDirEnv", v)

		return util.NewProcessExecutor(nil).RequiresGitDirEnv(commandArg(a.at(0))), nil
	})

	// JsonFile: each PHP instance has a maestro peer (json.new).
	r.Handle("json.new", func(v any) (any, error) {
		a := argsOf("json.new", v)
		out, ok, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		var fio io.IO
		if ok {
			fio = out
		}
		f, err := json.NewFile(a.str(0), nil, fio)
		if err != nil {
			return nil, err
		}

		return &service{v: f, class: classJsonFile}, nil
	})
	jsonMethod := func(name string, fn func(f *json.File, a args) (any, error)) {
		r.Handle("json."+name, func(v any) (any, error) {
			a := argsOf("json."+name, v)
			f, err := receiver[*json.File](a)
			if err != nil {
				return nil, err
			}

			return fn(f, a)
		})
	}
	jsonMethod("read", func(f *json.File, _ args) (any, error) { return f.Read() })
	jsonMethod("write", func(f *json.File, a args) (any, error) {
		options := json.DefaultEncodeFlags
		if a.has(2) {
			options = php.JSONFlag(a.integer(2))
		}

		return nil, f.Write(a.arrayOrEmpty(1), options)
	})
	jsonMethod("validateSchema", func(f *json.File, a args) (any, error) {
		schema := json.StrictSchema
		if a.has(1) {
			schema = a.integer(1)
		}
		schemaFile, _ := a.nullableString(2)
		if err := f.ValidateSchema(schema, schemaFile); err != nil {
			return nil, err
		}

		return true, nil
	})
	r.Handle("json.validateJsonSchema", func(v any) (any, error) {
		a := argsOf("json.validateJsonSchema", v)
		schemaFile, _ := a.nullableString(3)
		if err := json.ValidateJSONSchema(a.str(0), a.at(1), a.integer(2), schemaFile); err != nil {
			return nil, err
		}

		return true, nil
	})
	r.Handle("json.encode", func(v any) (any, error) {
		a := argsOf("json.encode", v)
		options := json.DefaultEncodeFlags
		if a.has(1) {
			options = php.JSONFlag(a.integer(1))
		}
		indent := json.IndentDefault
		if a.has(2) {
			indent = a.str(2)
		}

		return json.Encode(a.at(0), options, indent)
	})
	r.Handle("json.parseJson", func(v any) (any, error) {
		a := argsOf("json.parseJson", v)
		if !a.has(0) {
			return nil, nil
		}
		file, _ := a.nullableString(1)

		return json.ParseJSON(a.str(0), file)
	})
	r.Handle("json.detectIndenting", func(v any) (any, error) {
		a := argsOf("json.detectIndenting", v)

		return json.DetectIndenting(a.str(0)), nil
	})
	r.Handle("json.validateSyntax", func(v any) (any, error) {
		a := argsOf("json.validateSyntax", v)
		file, _ := a.nullableString(1)
		if _, err := json.ParseJSON(a.str(0), file); err != nil {
			return nil, err
		}

		return true, nil
	})
}

var _ rpc.Object = (*service)(nil)

// executeWithCallback is ProcessExecutor::execute($command, $callable):
// the PHP callable gets each chunk of output with its type, in order, on
// the goroutine waiting for the process, which holds the PHP baton (as
// Symfony's wait() calls it). What the callable throws ends the call once
// the process is done.
func (r *Runtime) executeWithCallback(pe *util.ProcessExecutor, command util.Command, cwd string, callable any) (int, error) {
	var callErr error
	code, err := pe.ExecuteFunc(command, func(typ, buf string) {
		if callErr == nil {
			_, callErr = r.Call("callable.invoke", php.ArrayOf("callable", callable, "args", php.ListOf(typ, buf)))
		}
	}, cwd)
	if callErr != nil {
		return 0, callErr
	}

	return code, err
}

// registerManipulator registers JsonManipulator's methods: each PHP
// instance has a maestro peer (json.newManipulator) whose methods are
// json.manipulate's.
func (r *Runtime) registerManipulator() {
	r.Handle("json.newManipulator", func(v any) (any, error) {
		a := argsOf("json.newManipulator", v)
		m, err := json.NewManipulator(a.str(0))
		if err != nil {
			return nil, err
		}

		return &service{v: m, class: `Composer\Json\JsonManipulator`}, nil
	})
	r.Handle("json.manipulate", func(v any) (any, error) {
		a := argsOf("json.manipulate", v)
		m, err := receiver[*json.Manipulator](a)
		if err != nil {
			return nil, err
		}
		p := args{method: "json.manipulate " + a.str(1), list: a.arrayOrEmpty(2).Values()}

		switch a.str(1) {
		case "getContents":
			return m.Contents(), nil
		case "addConfigSetting":
			return m.AddConfigSetting(p.str(0), p.at(1))
		case "addLink":
			return m.AddLink(p.str(0), p.str(1), p.str(2), p.boolean(3))
		case "addListItem":
			return m.AddListItem(p.str(0), p.at(1), p.boolean(2))
		case "addMainKey":
			return m.AddMainKey(p.str(0), p.at(1))
		case "addProperty":
			return m.AddProperty(p.str(0), p.at(1))
		case "addRepository":
			return m.AddRepository(p.str(0), p.at(1), p.boolean(2))
		case "addSubNode":
			return m.AddSubNode(p.str(0), p.str(1), p.at(2), p.boolean(3))
		case "changeEmptyMainKeyFromAssocToList":
			return m.ChangeEmptyMainKeyFromAssocToList(p.str(0))
		case "insertListItem":
			return m.InsertListItem(p.str(0), p.at(1), p.integer(2))
		case "insertRepository":
			return m.InsertRepository(p.str(0), p.at(1), p.str(2), p.integer(3))
		case "removeConfigSetting":
			return m.RemoveConfigSetting(p.str(0))
		case "removeListItem":
			return m.RemoveListItem(p.str(0), p.integer(1))
		case "removeMainKey":
			return m.RemoveMainKey(p.str(0))
		case "removeMainKeyIfEmpty":
			return m.RemoveMainKeyIfEmpty(p.str(0))
		case "removeProperty":
			return m.RemoveProperty(p.str(0))
		case "removeRepository":
			return m.RemoveRepository(p.str(0))
		case "removeSubNode":
			return m.RemoveSubNode(p.str(0), p.str(1))
		case "setRepositoryUrl":
			return m.SetRepositoryURL(p.str(0), p.str(1))
		case "format":
			return m.Format(p.at(0), p.integer(1), p.boolean(2))
		case "detectIndenting":
			return nil, m.DetectIndenting()
		}

		return nil, a.errorf("JsonManipulator has no method %s", a.str(1))
	})
}

// registerConfigSources registers the `cfgsrc.*` methods: maestro's
// JsonConfigSources, and those PHP creates.
func (r *Runtime) registerConfigSources() {
	r.Handle("cfgsrc.new", func(v any) (any, error) {
		a := argsOf("cfgsrc.new", v)
		out, ok, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		var fio io.IO
		if ok {
			fio = out
		}
		f, err := json.NewFile(a.str(1), nil, fio)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, config.NewJSONConfigSource(f, a.boolean(3)))
	})
	method := func(name string, fn func(s config.ConfigSource, a args) error) {
		r.Handle("cfgsrc."+name, func(v any) (any, error) {
			a := argsOf("cfgsrc."+name, v)
			s, err := receiver[config.ConfigSource](a)
			if err != nil {
				return nil, err
			}

			return nil, fn(s, a)
		})
	}
	r.Handle("cfgsrc.getName", func(v any) (any, error) {
		a := argsOf("cfgsrc.getName", v)
		s, err := receiver[config.ConfigSource](a)
		if err != nil {
			return nil, err
		}

		return s.Name(), nil
	})
	method("addRepository", func(s config.ConfigSource, a args) error {
		return s.AddRepository(a.str(1), a.at(2), !a.has(3) || a.boolean(3))
	})
	method("insertRepository", func(s config.ConfigSource, a args) error {
		return s.InsertRepository(a.str(1), a.at(2), a.str(3), a.integer(4))
	})
	method("setRepositoryUrl", func(s config.ConfigSource, a args) error { return s.SetRepositoryURL(a.str(1), a.str(2)) })
	method("removeRepository", func(s config.ConfigSource, a args) error { return s.RemoveRepository(a.str(1)) })
	method("addConfigSetting", func(s config.ConfigSource, a args) error { return s.AddConfigSetting(a.str(1), a.at(2)) })
	method("removeConfigSetting", func(s config.ConfigSource, a args) error { return s.RemoveConfigSetting(a.str(1)) })
	method("addProperty", func(s config.ConfigSource, a args) error { return s.AddProperty(a.str(1), a.at(2)) })
	method("removeProperty", func(s config.ConfigSource, a args) error { return s.RemoveProperty(a.str(1)) })
	method("addLink", func(s config.ConfigSource, a args) error { return s.AddLink(a.str(1), a.str(2), a.str(3)) })
	method("removeLink", func(s config.ConfigSource, a args) error { return s.RemoveLink(a.str(1), a.str(2)) })
}

// passwordArg is ProcessExecutor::outputCommandRun's --password pattern.
var passwordArg = php.MustCompile(`{--password (.*[^\\]') }`)

// registerAsyncProcesses registers `proc.describeAsync`: the debug line of
// an asynchronous process PHP code starts (outputCommandRun), with
// maestro's URL sanitizing.
func (r *Runtime) registerAsyncProcesses() {
	r.Handle("proc.describeAsync", func(v any) (any, error) {
		a := argsOf("proc.describeAsync", v)
		safe, _, err := passwordArg.Replace(util.SanitizeURL(commandArg(a.at(0)).String()), `--password '***' `, -1)
		if err != nil {
			return nil, err
		}
		cwd := "CWD"
		if c, ok := a.nullableString(1); ok && php.ToBool(c) {
			cwd = c
		}

		return "Executing async command (" + cwd + "): " + safe, nil
	})
}
