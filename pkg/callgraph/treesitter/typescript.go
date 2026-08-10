package treesitter

// Tree-sitter queries for TypeScript function definitions and calls.

const typescriptFuncQuery = `
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

(function_declaration
  name: (identifier) @func_name
  parameters: (formal_parameters) @params) @func_def

(method_definition
  name: (property_identifier) @func_name
  parameters: (formal_parameters) @params) @func_def

(arrow_function
  parameters: (formal_parameters) @params) @arrow_func
`

const typescriptCallQuery = `
(call_expression
  function: (identifier) @call_name) @call

(call_expression
  function: (member_expression
    object: (identifier) @obj
    property: (property_identifier) @method_name)) @call
`

// extractTypeScriptFuncName extracts the function name from a TypeScript query match.
func extractTypeScriptFuncName(captureName string) string {
	return captureName
}
