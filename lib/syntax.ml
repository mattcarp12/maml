type expr =
  | Int of int
  | Float of float
  | String of string
  | Plus of (expr * expr)
  | Minus of (expr * expr)
  | Mult of (expr * expr)
  | Div of (expr * expr)
  | True
  | False

type decl = Decl_Const of (string * expr) | Decl_Func of string
type program = Program of decl list

let rec print_expr e =
  match e with
  | Int i -> print_int i
  | Float f -> print_float f
  | String s -> print_string s
  | Plus (e1, e2) ->
      print_expr e1;
      print_char '+';
      print_expr e2
  | Minus (e1, e2) ->
      print_expr e1;
      print_char '-';
      print_expr e2
  | Mult (e1, e2) ->
      print_expr e1;
      print_char '*';
      print_expr e2
  | Div (e1, e2) ->
      print_expr e1;
      print_char '/';
      print_expr e2
  | True | False -> print_string "Bool"

let print_decl d =
  (match d with
  | Decl_Const (a, b) ->
      print_string ("Decl_Const: name = " ^ a ^ ", value = ");
      print_expr b
  | Decl_Func f -> print_string ("Decl_Func: " ^ f));
  print_newline ()

let print_program p =
  print_newline ();
  print_endline "Program: ";
  let rec print_decls dl =
    match dl with
    | [] -> ()
    | x :: y ->
        print_char '\t';
        print_decl x;
        print_decls y
  in
  match p with Program dl -> print_decls dl
