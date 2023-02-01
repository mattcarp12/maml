%{
open Syntax
%}
%token <int> INT
%token <string> ID
%token EQUALS
%token LEFT_BRACE
%token RIGHT_BRACE
%token LEFT_PAREN
%token RIGHT_PAREN
%token LET
%token EOF

%start <Syntax.program> program
%%

(* Menhir expresses grammars as context-free grammars *)

program :
    | f = func; EOF { Program f }
    ;

func :
    | LET id=ID EQUALS LEFT_PAREN RIGHT_PAREN LEFT_BRACE e=expr RIGHT_BRACE { Function(id,e) }

expr :
    | i = INT { Int i }
    ;
