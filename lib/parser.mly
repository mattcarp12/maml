%{
open Syntax
%}
%token <int> INT
%token <float> FLOAT
%token <string> ID
%token <string> STRING
%token TRUE
%token FALSE

%token EQUALS
%token PLUS
%token MINUS
%token MULT
%token DIV

// %token LEFT_BRACE
// %token RIGHT_BRACE

%token LET

%token EOF

%start <Syntax.program> program
%%

// Menhir expresses grammars as context-free grammars

program :
    | v = decl*; EOF {Program v}
    ;

decl : 
    | LET; i=ID; EQUALS; v=expr {Decl_Const(i, v)}
    ;

expr :
    | i = INT {Int i}
    | f = FLOAT {Float f}
    | s = STRING {String s}
    | FALSE {False}
    | TRUE {True}
    | e1 = expr; PLUS; e2 = expr {Plus(e1, e2)}
    | e1 = expr; MINUS; e2 = expr {Minus(e1, e2)}
    | e1 = expr; MULT; e2 = expr {Mult(e1, e2)}
    | e1 = expr; DIV; e2 = expr {Div(e1, e2)}
    ;
