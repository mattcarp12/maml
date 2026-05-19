const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{ .preferred_optimize_mode = .ReleaseFast });

    // 1. Fetch the mimalloc dependency declared in your build.zig.zon
    const mimalloc_dep = b.dependency("mimalloc", .{});

    const lib = b.addLibrary(.{
        .linkage = .static,
        .name = "mamlrt",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/runtime.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    // 2. Tell Zig where the mimalloc C header files are INSIDE the dependency cache
    lib.root_module.addIncludePath(mimalloc_dep.path("include"));

    // 3. Compile mimalloc directly out of the global dependency cache
    lib.root_module.addCSourceFile(.{
        .file = mimalloc_dep.path("src/static.c"),
        .flags = &[_][]const u8{
            "-DMI_SECURE=0", // Standard performance flag
            "-fno-sanitize=undefined", // Tell Zig to back off with safety checks for this C library
        },
    });

    // 4. Link the standard C library (mimalloc needs it to request raw memory pages from the OS)
    lib.root_module.link_libc = true;

    // 5. Install the final .a file into the zig-out/ folder
    b.installArtifact(lib);
}
