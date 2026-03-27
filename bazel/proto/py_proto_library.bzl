"""Local Python proto rule that avoids protobuf's python/dist packaging graph."""

load("@protobuf//bazel/common:proto_common.bzl", "proto_common")
load("@protobuf//bazel/common:proto_info.bzl", "ProtoInfo")
load("@protobuf//bazel/common:proto_lang_toolchain_info.bzl", "ProtoLangToolchainInfo")
load("@rules_python//python:py_info.bzl", "PyInfo")

_PyProtoInfo = provider(
    doc = "Information needed by the local Python proto rule.",
    fields = {
        "imports": "Additional PYTHONPATH entries required by generated protos.",
        "runfiles_from_proto_deps": "Runfiles collected from implicit proto deps.",
        "transitive_sources": "Generated Python sources.",
    },
)

def _filter_provider(provider, *attrs):
    return [dep[provider] for attr in attrs for dep in attr if provider in dep]

def _py_proto_aspect_impl(target, ctx):
    for proto in target[ProtoInfo].direct_sources:
        if proto.is_source and "-" in proto.dirname:
            fail("Cannot generate Python code for a .proto whose path contains '-' ({}).".format(
                proto.path,
            ))

    proto_lang_toolchain_info = ProtoLangToolchainInfo(
        out_replacement_format_flag = "--python_out=%s",
        output_files = "legacy",
        plugin_format_flag = "",
        plugin = None,
        runtime = None,
        provided_proto_sources = [],
        proto_compiler = ctx.executable._protoc,
        protoc_opts = [],
        progress_message = "Generating Python proto_library %{label}",
        mnemonic = "GenProto",
        allowlist_different_package = None,
        toolchain_type = None,
    )

    generated_sources = []
    proto_info = target[ProtoInfo]
    proto_root = proto_info.proto_source_root
    if proto_info.direct_sources:
        generated_sources = proto_common.declare_generated_files(
            actions = ctx.actions,
            proto_info = proto_info,
            extension = "_pb2.py",
            name_mapper = lambda name: name.replace("-", "_").replace(".", "/"),
        )

        if proto_root.startswith(ctx.bin_dir.path):
            proto_root = proto_root[len(ctx.bin_dir.path) + 1:]

        plugin_output = ctx.bin_dir.path + "/" + proto_root
        proto_root = ctx.workspace_name + "/" + proto_root

        proto_common.compile(
            actions = ctx.actions,
            proto_info = proto_info,
            proto_lang_toolchain_info = proto_lang_toolchain_info,
            generated_files = generated_sources,
            plugin_output = plugin_output,
        )

    deps = _filter_provider(_PyProtoInfo, getattr(ctx.rule.attr, "deps", []))
    runtime = ctx.attr._runtime
    runfiles_from_proto_deps = depset(
        transitive = [runtime[DefaultInfo].default_runfiles.files] +
                     [dep.runfiles_from_proto_deps for dep in deps],
    )
    transitive_sources = depset(
        direct = generated_sources,
        transitive = [dep.transitive_sources for dep in deps],
    )

    return [
        _PyProtoInfo(
            imports = depset(
                [proto_root] if "_virtual_imports" in proto_root else [],
                transitive = [runtime[PyInfo].imports] + [dep.imports for dep in deps],
            ),
            runfiles_from_proto_deps = runfiles_from_proto_deps,
            transitive_sources = transitive_sources,
        ),
    ]

_py_proto_aspect = aspect(
    implementation = _py_proto_aspect_impl,
    attrs = {
        "_protoc": attr.label(
            executable = True,
            cfg = "exec",
            default = "@protobuf//:protoc",
        ),
        "_runtime": attr.label(
            default = "//bazel/proto:protobuf_runtime",
            providers = [PyInfo],
        ),
    },
    attr_aspects = ["deps"],
    required_providers = [ProtoInfo],
    provides = [_PyProtoInfo],
)

def _py_proto_library_rule(ctx):
    if not ctx.attr.deps:
        fail("'deps' attribute mustn't be empty.")

    pyproto_infos = _filter_provider(_PyProtoInfo, ctx.attr.deps)
    default_outputs = depset(
        transitive = [info.transitive_sources for info in pyproto_infos],
    )

    return [
        DefaultInfo(
            files = default_outputs,
            default_runfiles = ctx.runfiles(
                transitive_files = depset(
                    transitive = [default_outputs] +
                                 [info.runfiles_from_proto_deps for info in pyproto_infos],
                ),
            ),
        ),
        OutputGroupInfo(default = depset()),
        PyInfo(
            transitive_sources = default_outputs,
            imports = depset(transitive = [info.imports for info in pyproto_infos]),
            has_py2_only_sources = False,
            has_py3_only_sources = False,
        ),
    ]

py_proto_library = rule(
    implementation = _py_proto_library_rule,
    attrs = {
        "deps": attr.label_list(
            providers = [ProtoInfo],
            aspects = [_py_proto_aspect],
        ),
    },
    provides = [PyInfo],
)
