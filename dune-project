(lang dune 3.6)

(name maml)

(generate_opam_files true)

(source
 (github mattcarp12/maml))

(authors "Matt Carpenter")

(maintainers "Matt Carpenter")

(license LICENSE)

(documentation https://url/to/documentation)

(package
 (name maml)
 (synopsis "A short synopsis")
 (description "A longer description")
 (depends ocaml dune)
 (tags
  (topics "to describe" your project)))

(using menhir 2.1)

; See the complete stanza docs at https://dune.readthedocs.io/en/stable/dune-files.html#dune-project
