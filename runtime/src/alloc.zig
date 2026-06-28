pub extern "C" fn mi_malloc(size: usize) ?*anyopaque;
pub extern "C" fn mi_realloc(p: ?*anyopaque, size: usize) ?*anyopaque;
pub extern "C" fn mi_free(p: ?*anyopaque) void;

pub export fn maml_alloc(size: usize) ?*anyopaque {
    return mi_malloc(size);
}

pub export fn maml_free(data_ptr: ?*anyopaque) void {
    if (data_ptr == null) return;
    mi_free(data_ptr);
}
