package test.cortex

import rego.v1

default allow := false

readonly := {"ls", "cat", "git status"}

allow if {
	input.action.name == "bash"
	input.action.properties.command in readonly
}

allow if {
	input.action.name == "write"
	startswith(input.resource.id, input.context.workspace)
}

allow if {
	input.action.name == "delegate"
	input.context.approval.granted == true
}

allow if {
	input.action.name == "read"
	input.subject.type == "user"
	"lalter:user" in input.subject.properties.roles
}

reason := "approval_required" if {
	input.action.name in {"delegate", "schedule", "send"}
	not input.context.approval.granted
}

context := {"workspace": input.context.workspace} if {
	input.action.name == "write"
}
