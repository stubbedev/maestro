// Composer\Util\Filesystem, ProcessExecutor and Json\JsonFile
// (docs/PLUGINS.md §4.8, §4.10): `fs.*`, `proc.*` and `json.*`, run by
// maestro's ports.

package plugin

import (
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
		out, ok, err := ioParam(a, 2)
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
		out, ok, err := ioParam(a, 1)
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
