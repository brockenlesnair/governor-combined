package treesitter

// Tree-sitter queries for Rust function definitions and calls.

const rustFuncQuery = `
(function_item
  name: (identifier) @func_name
  parameters: (parameters) @params) @func_def
`

const rustCallQuery = `
(call_expression
  function: (identifier) @call_name) @call

(call_expression
  function: (field_expression
    field: (field_identifier) @method_name)) @call

(call_expression
  function: (scoped_identifier
    path: (identifier) @obj
    name: (identifier) @call_name)) @call
`

// extractRustFuncName extracts the function name from a Rust query match.
func extractRustFuncName(captureName string) string {
	return captureName
}
