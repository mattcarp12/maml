{
open Lexing
open Parser

exception SyntaxError of string

let next_line lexbuf =
  let pos = lexbuf.lex_curr_p in
  lexbuf.lex_curr_p <-
    { pos with pos_bol = lexbuf.lex_curr_pos;
               pos_lnum = pos.pos_lnum + 1
    }
}

let digit = ['0'-'9']
let alpha = ['a'-'z' 'A'-'Z']
let int = '-'? digit+  
let whitespace = [' ' '\t']+
let newline = '\r' | '\n' | "\r\n"

rule token = parse
  | whitespace {token lexbuf}
  | newline {next_line lexbuf; token lexbuf}
  | int { INT (int_of_string(lexeme lexbuf)) }
  | "=" {EQUALS}
  | "{" {LEFT_BRACE}
  | "}" {RIGHT_BRACE}
  | "(" {LEFT_PAREN}
  | ")" {RIGHT_PAREN}
  | "let" {LET}
  | (alpha) (alpha|digit|'_')* { ID (lexeme lexbuf) }
  | _ { raise (SyntaxError ("Unexpected char: " ^ Lexing.lexeme lexbuf)) }
  | eof {EOF}
