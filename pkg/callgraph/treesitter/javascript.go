package treesitter

// Tree-sitter queries for JavaScript function definitions and calls.
// JavaScript shares the same grammar structure as TypeScript (without type annotations).

const javascriptFuncQuery = `
(function_declaration
  name: (identifier) @func_name
  parameters: (formal_parameters) @params) @func_def

(method_definition
  name: (property_identifier) @func_name
  parameters: (formal_parameters) @params) @func_def

(lexical_declaration
  (variable_declarator
    name: (identifier) @func_name
    value: (arrow_function
      parameters: (formal_parameters) @params))) @func_def

(lexical_declaration
  (variable_declarator
    name: (identifier) @func_name
    value: (function_expression
      parameters: (formal_parameters) @params))) @func_def
`

const javascriptCallQuery = `
(call_expression
  function: (identifier) @call_name) @call

(call_expression
  function: (member_expression
    object: (identifier) @obj
    property: (property_identifier) @method_name)) @call
`
