open Maml

(* Open file and read it *)
let f = "test/test1.maml"

let print_position outx (lexbuf : Lexing.lexbuf) =
  let pos = lexbuf.lex_curr_p in
  Printf.fprintf outx "%s:%d:%d" pos.pos_fname pos.pos_lnum
    (pos.pos_cnum - pos.pos_bol + 1)

let parse_with_error lexbuf =
  try Ok (Parser.program Lexer.token lexbuf) with
  | Lexer.SyntaxError msg ->
      Printf.fprintf stderr "%a: %s\n" print_position lexbuf msg;
      Error msg
  | Parser.Error ->
      Printf.fprintf stderr "%a: syntax error\n" print_position lexbuf;
      (* exit (-1) *)
      Error "syntax"

let parse source =
  let inx = open_in source in
  let lexbuf = Lexing.from_channel inx in
  lexbuf.lex_curr_p <- { lexbuf.lex_curr_p with pos_fname = source };
  (* Parser.program Lexer.token lexbuf *)
  match parse_with_error lexbuf with
  | Ok program -> Codegen.codegen program
  | Error msg -> Printf.printf "Error: %s\n" msg

let _ = parse f
