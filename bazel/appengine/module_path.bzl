# App Engine deploy only needs source files from this module. Third-party
# dependencies are resolved by the copied go.mod and go.sum.

load("@bazel_skylib//lib:paths.bzl", "paths")
load(
    "@rules_go//go/private:providers.bzl",
    "GoArchive",
    "GoPath",
    "effective_importpath_pkgpath",
)

def _is_in_module(importpath, module_prefix):
    return importpath == module_prefix or importpath.startswith(module_prefix + "/")

def _go_module_path_impl(ctx):
    mode_to_deps = {}
    for dep in ctx.attr.deps:
        archive = dep[GoArchive]
        mode = archive.source.mode
        if mode not in mode_to_deps:
            mode_to_deps[mode] = []
        mode_to_deps[mode].append(archive)

    mode_to_archive = {}
    for mode, archives in mode_to_deps.items():
        direct = [a.data for a in archives]
        transitive = []
        if ctx.attr.include_transitive:
            transitive = [a.transitive for a in archives]
        mode_to_archive[mode] = depset(direct = direct, transitive = transitive)

    pkg_map = {}
    for mode, archives in mode_to_archive.items():
        for archive in archives.to_list():
            importpath, pkgpath = effective_importpath_pkgpath(archive)
            if importpath == "" or not _is_in_module(importpath, ctx.attr.module_prefix):
                continue

            pkg = struct(
                importpath = importpath,
                dir = "src/" + pkgpath,
                srcs = list(archive.srcs),
                runfiles = archive.runfiles,
                embedsrcs = list(archive._embedsrcs),
                pkgs = {mode: archive.file},
            )
            if pkgpath in pkg_map:
                pkg = _merge_pkg(pkg_map[pkgpath], pkg)
            pkg_map[pkgpath] = pkg

    inputs = []
    manifest_entries = []
    manifest_entry_map = {}
    for pkg in pkg_map.values():
        src_dir = None

        for f in pkg.srcs:
            src_dir = f.dirname
            dst = pkg.dir + "/" + f.basename
            _add_manifest_entry(manifest_entries, manifest_entry_map, inputs, f, dst)
        for f in pkg.embedsrcs:
            if src_dir == None:
                fail("cannot relativize {}: src_dir is unset".format(f.path))
            embedpath = paths.relativize(f.path, f.root.path)
            dst = pkg.dir + "/" + paths.relativize(
                embedpath.lstrip(ctx.bin_dir.path + "/"),
                src_dir.lstrip(ctx.bin_dir.path + "/"),
            )
            _add_manifest_entry(manifest_entries, manifest_entry_map, inputs, f, dst)

    if ctx.attr.include_pkg:
        for pkg in pkg_map.values():
            for mode, f in pkg.pkgs.items():
                installsuffix = mode.goos + "_" + mode.goarch
                dst = "pkg/" + installsuffix + "/" + pkg.dir[len("src/"):] + ".a"
                _add_manifest_entry(manifest_entries, manifest_entry_map, inputs, f, dst)

    if ctx.attr.include_data:
        for pkg in pkg_map.values():
            for f in pkg.runfiles.files.to_list():
                parts = f.path.split("/")
                if "testdata" in parts:
                    i = parts.index("testdata")
                    dst = pkg.dir + "/" + "/".join(parts[i:])
                else:
                    dst = pkg.dir + "/" + f.basename
                _add_manifest_entry(manifest_entries, manifest_entry_map, inputs, f, dst)

    for f in ctx.files.data:
        _add_manifest_entry(manifest_entries, manifest_entry_map, inputs, f, f.basename)

    manifest_file = ctx.actions.declare_file(ctx.label.name + "~manifest")
    manifest_entries_json = [json.encode(e) for e in manifest_entries]
    manifest_content = "[\n  " + ",\n  ".join(manifest_entries_json) + "\n]"
    ctx.actions.write(manifest_file, manifest_content)
    inputs.append(manifest_file)

    if ctx.attr.mode == "archive":
        out = ctx.actions.declare_file(ctx.label.name + ".zip")
        out_path = out.path
        out_short_path = out.short_path
        outputs = [out]
        out_file = out
    elif ctx.attr.mode == "copy":
        out = ctx.actions.declare_directory(ctx.label.name)
        out_path = out.path
        out_short_path = out.short_path
        outputs = [out]
        out_file = out
    else:
        outputs = [
            ctx.actions.declare_file(ctx.label.name + "/" + e.dst)
            for e in manifest_entries
        ]
        tag = ctx.actions.declare_file(ctx.label.name + "/.tag")
        ctx.actions.write(tag, "")
        out_path = tag.dirname
        out_short_path = tag.short_path.rpartition("/")[0]
        out_file = tag

    args = ctx.actions.args()
    args.add("-manifest", manifest_file)
    args.add("-out", out_path)
    args.add("-mode", ctx.attr.mode)
    ctx.actions.run(
        outputs = outputs,
        inputs = inputs,
        mnemonic = "GoModulePath",
        executable = ctx.executable._go_path,
        arguments = [args],
    )

    return [
        DefaultInfo(
            files = depset(outputs),
            runfiles = ctx.runfiles(files = outputs),
        ),
        GoPath(
            gopath = out_short_path,
            gopath_file = out_file,
            packages = pkg_map.values(),
        ),
    ]

go_module_path = rule(
    implementation = _go_module_path_impl,
    attrs = {
        "data": attr.label_list(allow_files = True),
        "deps": attr.label_list(providers = [GoArchive]),
        "include_data": attr.bool(default = True),
        "include_pkg": attr.bool(default = False),
        "include_transitive": attr.bool(default = True),
        "mode": attr.string(
            default = "copy",
            values = ["archive", "copy", "link"],
        ),
        "module_prefix": attr.string(mandatory = True),
        "_go_path": attr.label(
            default = Label("@rules_go//go/tools/builders:go_path"),
            executable = True,
            cfg = "exec",
        ),
    },
)

def _merge_pkg(x, y):
    x_srcs = {f.path: None for f in x.srcs}
    x_embedsrcs = {f.path: None for f in x.embedsrcs}

    pkgs = dict()
    pkgs.update(x.pkgs)
    pkgs.update(y.pkgs)

    return struct(
        importpath = x.importpath,
        dir = x.dir,
        srcs = x.srcs + [f for f in y.srcs if f.path not in x_srcs],
        runfiles = x.runfiles.merge(y.runfiles),
        embedsrcs = x.embedsrcs + [f for f in y.embedsrcs if f.path not in x_embedsrcs],
        pkgs = pkgs,
    )

def _add_manifest_entry(entries, entry_map, inputs, src, dst):
    if dst in entry_map:
        if entry_map[dst] != src.path:
            fail("{}: references multiple files ({} and {})".format(dst, entry_map[dst], src.path))
        return
    entries.append(struct(src = src.path, dst = dst))
    entry_map[dst] = src.path
    inputs.append(src)
