const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    // 1. Tell Zig to create a Static Library named "mamlrt" (MAML Runtime)
    // using the newer Zig 0.14+ addLibrary API
    const lib = b.addLibrary(.{
        .linkage = .static,
        .name = "mamlrt",
        .root_module = b.createModule(.{
            .root_source_file = b.path("runtime.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    // 2. Tell Zig where the mimalloc C header files are
    lib.root_module.addIncludePath(b.path("mimalloc/include"));

    // 3. Compile mimalloc!
    lib.root_module.addCSourceFile(.{
        .file = b.path("mimalloc/src/static.c"),
        .flags = &[_][]const u8{
            "-DMI_SECURE=0", // Standard performance flag
            "-fno-sanitize=undefined", // <--- NEW: Tell Zig to back off with the safety checks!
        },
    });

    // 4. Link the standard C library (mimalloc needs it to ask the OS for raw page memory)
    lib.root_module.link_libc = true;

    // 5. Install the final .a file into the zig-out/ folder
    b.installArtifact(lib);
}
