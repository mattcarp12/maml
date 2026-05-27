Program         = { Declaration } ;
Declaration     = [ "async" ] FnDecl | TypeDecl ;

FnDecl          = "fn" Identifier "(" [ ParamList ] ")" [ TypeExpr ] BlockStmt ;
ParamList       = Param { "," Param } ;
Param           = [ "mut" | "own" ] Identifier TypeExpr ;

TypeDecl        = "type" Identifier "=" ( ProductType | SumType ) ;
ProductType     = "{" [ FieldDecl { "," FieldDecl } ] "}" ;
FieldDecl       = Identifier TypeExpr ;
SumType         = SeparatorVariant { SeparatorVariant } ;
SeparatorVariant= "|" Identifier [ ProductType ] ;

TypeExpr        = NamedType | SliceType | ArrayType | GenericType ;
NamedType       = Identifier ;
SliceType       = "[" "]" TypeExpr ;
ArrayType       = "[" IntLiteral "]" TypeExpr ;
GenericType     = Identifier "<" TypeExpr { "," TypeExpr } ">" ;

BlockStmt       = "{" { Stmt } "}" ;
Stmt            = DeclareStmt | AssignStmt | ReturnStmt | YieldStmt | ForStmt 
                | BreakStmt | ContinueStmt | ExprStmt ;

DeclareStmt     = [ "mut" ] Identifier ":=" Expression ;
AssignStmt      = Expression "=" Expression ;
ReturnStmt      = "return" [ Expression ] ;
YieldStmt       = "=>" Expression ;
ExprStmt        = Expression ;

ForStmt         = "for" ( WhileCondition | CStyleLoop ) BlockStmt ;
WhileCondition  = Expression ;
CStyleLoop      = Stmt ";" Expression ";" Stmt ;

BreakStmt       = "break" ;
ContinueStmt    = "continue" ;

Expression      = PrimaryExpr | InfixExpr | PrefixExpr ;

PrimaryExpr     = Identifier | IntLiteral | FloatLiteral | StringLiteral | BoolLiteral
                | GroupedExpr | IfExpr | MatchExpr | ArrayLiteral | StructLiteral 
                | CallExpr | IndexExpr | SliceExpr | FieldAccess | AwaitExpr ;

GroupedExpr     = "(" Expression ")" ;
IfExpr          = "if" Expression BlockStmt [ "else" BlockStmt ] ;

MatchExpr       = "match" Expression "{" { MatchArm } "}" ;
MatchArm        = "case" Pattern BlockStmt ;
Pattern         = "_" | LiteralPattern | VariantPattern ;
LiteralPattern  = IntLiteral | BoolLiteral ;
VariantPattern  = Identifier [ "(" Identifier ")" ] 
                | Identifier "{" FieldBinding { "," FieldBinding } "}" ;
FieldBinding    = Identifier ":" Identifier ;

ArrayLiteral    = "[" [ Expression { "," Expression } ] "]" ;
StructLiteral   = Identifier "{" [ StructField { "," StructField } ] "}" ;
StructField     = Identifier ":" Expression ;

CallExpr        = Expression "(" [ CallArgs ] ")" ;
CallArgs        = CallArg { "," CallArg } ;
CallArg         = [ "mut" | "own" ] Expression ;

IndexExpr       = Expression "[" Expression "]" ;
SliceExpr       = Expression "[" [ Expression ] ":" [ Expression ] "]" ;
FieldAccess     = Expression "." Identifier ;
AwaitExpr       = "await" Expression ;

PrefixExpr      = ( "-" | "!" ) Expression ;
InfixExpr       = Expression Op Expression ;
Op              = "+" | "-" | "*" | "/" | "%" | "==" | "!=" | "<" | "<=" | ">" | ">=" | "&&" | "||" ;

IDENT       = letter { letter | digit | "_" } ;
INT_LIT     = digit { digit } ;
FLOAT_LIT   = digit { digit } "." digit { digit } ;
BOOL_LIT    = "true" | "false" ;
STRING_LIT  = '"' { any char except '"' } '"' ;

(* comments are // to end of line and are stripped *)
(* newlines are significant for ASI when they follow: IDENT, INT, FLOAT, STRING, 
   BOOL, ")", "}", "]", "return", "=>" — otherwise treated as whitespace *)