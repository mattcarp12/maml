let print_header out =
  Printf.fprintf out "global _start\n";
  Printf.fprintf out "section .text\n";
  ()

let print_start out =
  Printf.fprintf out "\n";
  Printf.fprintf out "_start:\n";
  Printf.fprintf out "\t mov rax, 1  ; system call for write\n";
  Printf.fprintf out "\t mov rdi, 1  ; file handle 1 is stdout\n";
  Printf.fprintf out "\t mov rsi, message  ; address of string to output\n";
  Printf.fprintf out "\t mov rdx, message_len  ; number of bytes\n";
  Printf.fprintf out "\t syscall ; invoke operating system \n";
  Printf.fprintf out "\t mov rax, 60 ; system call for exit \n";
  Printf.fprintf out "\t xor rdi,rdi ; exit code 0 \n";
  Printf.fprintf out "\t syscall ; invoke the operating system\n";
  ()

let print_data_section out =
  Printf.fprintf out "\n";
  Printf.fprintf out "section .data\n"

let print_data out i =
  Printf.fprintf out "message: db \"%d\",10\n" i;
  Printf.fprintf out "message_len: equ $ - message"

let codegen program =
  let out = open_out "test/test_out.asm" in

  print_header out;
  print_start out;
  print_data_section out;

  match program with
  | Syntax.Program (Function (_, Int i)) ->
      print_data out i;
      ()
