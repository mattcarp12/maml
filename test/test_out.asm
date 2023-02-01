global _start
section .text

_start:
	 mov rax, 1  ; system call for write
	 mov rdi, 1  ; file handle 1 is stdout
	 mov rsi, message  ; address of string to output
	 mov rdx, message_len  ; number of bytes
	 syscall ; invoke operating system 
	 mov rax, 60 ; system call for exit 
	 xor rdi,rdi ; exit code 0 
	 syscall ; invoke the operating system

section .data
message: db "3",10
message_len: equ $ - message