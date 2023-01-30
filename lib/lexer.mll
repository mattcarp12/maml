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

(* Define helper regexes *)
let digit = ['0'-'9']
let alpha = ['a'-'z' 'A'-'Z']

let int = '-'? digit+  (* regex for integers *)
let whitespace = [' ' '\t']+
let newline = '\r' | '\n' | "\r\n"


rule token = parse
  | whitespace {token lexbuf}
  | newline {next_line lexbuf; token lexbuf}
  | int { INT (int_of_string(lexeme lexbuf)) }
  | "true" {TRUE}
  | "false" {FALSE}
  | "=" {EQUALS}
  | "+" {PLUS}
  | "-" {MINUS}
  | "*" {MULT}
  | "/" {DIV}
  (* | "{" {LEFT_BRACE} *)
  (* | "}" {RIGHT_BRACE} *)
  | "let" {LET}
  | (alpha) (alpha|digit|'_')* { ID (lexeme lexbuf) }
  | _ { raise (SyntaxError ("Unexpected char: " ^ Lexing.lexeme lexbuf)) }
  | eof {EOF}


(* 
rule read_token =
  parse
  | "(" { LPAREN }
  (* ... keywords and other characters' regexes *)
  | "printf" {PRINTF }
  | whitespace { read_token lexbuf }
  | "//" { single_line_comment lexbuf (* use our comment rule for rest of line *) }
  | "/*" { multi_line_comment lexbuf }
  | int { INT (int_of_string (Lexing.lexeme lexbuf))}
  | id { ID (Lexing.lexeme lexbuf) }
    | '"'      { read_string (Buffer.create 17) lexbuf }
  | newline { next_line lexbuf; read_token lexbuf }
  | eof { EOF }
  | _ {raise (SyntaxError ("Lexer - Illegal character: " ^ Lexing.lexeme lexbuf)) }

and read_single_line_comment = parse
  | newline { next_line lexbuf; read_token lexbuf }
  | eof { EOF }
  | _ { read_single_line_comment lexbuf }

and read_multi_line_comment = parse
  | "*/" { read_token lexbuf }
  | newline { next_line lexbuf; read_multi_line_comment lexbuf }
  | eof { raise (SyntaxError ("Lexer - Unexpected EOF - please terminate your comment.")) }
  | _ { read_multi_line_comment lexbuf }

and read_string buf = parse
  | '"'       { STRING (Buffer.contents buf) }
  | '\\' 'n'  { Buffer.add_char buf '\n'; read_string buf lexbuf }
  (* ... Other regexes to handle escaping special characters *)
  | [^ '"' '\\']+
    { Buffer.add_string buf (Lexing.lexeme lexbuf);
      read_string buf lexbuf
    }
  | _ { raise (SyntaxError ("Illegal string character: " ^ Lexing.lexeme lexbuf)) }
  | eof { raise (SyntaxError ("String is not terminated")) }
 *)
