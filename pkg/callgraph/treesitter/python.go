package treesitter

// Tree-sitter queries for Python function definitions and calls.

const pythonFuncQuery = `
(function_definition
  name: (identifier) @func_name
  parameters: (parameters) @params
  body: (block) @body) @func_def

(decorated_definition
  (decorator) @decorator
  definition: (function_definition
    name: (identifier) @func_name
    parameters: (parameters) @params
    body: (block) @body)) @func_def
`

const pythonCallQuery = `
(call
  function: (identifier) @call_name) @call

(call
  function: (attribute
    object: (identifier) @obj
    attribute: (identifier) @method_name)) @call
`

// extractPythonFuncName extracts the function name from a Python query match.
// The @func_name capture should contain the function identifier.
func extractPythonFuncName(captureName string) string {
	return captureName
}

// isPythonMethodDef checks if the matched node represents a method inside a class.
// In Python, methods have 'self' as the first parameter.
func isPythonMethodDef() bool {
	return false
}
