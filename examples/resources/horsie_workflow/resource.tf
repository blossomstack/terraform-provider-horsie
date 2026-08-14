resource "horsie_workflow" "review" {
  name        = "review-and-fix"
  description = "Review a change, and loop back to the fixer until it passes."
  start       = "review"

  step {
    name   = "review"
    agent  = horsie_agent.reviewer.name
    prompt = "Review the change below and decide whether it is ready to merge."

    outcome {
      value       = "approved"
      description = "The change is ready to merge as it stands."
    }
    outcome {
      value       = "changes_requested"
      description = "Something has to change before this can merge."
    }

    result_field {
      name        = "blockers"
      kind        = "StringList"
      description = "What must change before this can merge. Empty when approved."
    }

    # Order matters: the first matching transition wins, so the catch-all is last.
    transition {
      to              = "fix"
      when_outcome_in = ["changes_requested"]
    }
    transition {
      to = "summarise"
    }
  }

  step {
    name           = "fix"
    agent          = horsie_agent.coder.name
    prompt         = "Address the blockers below, then hand the change back for review."
    max_iterations = 40

    transition {
      to = "review"
    }
  }

  # A step with no transitions ends the run, carrying its result as the run's.
  step {
    name   = "summarise"
    agent  = horsie_agent.reviewer.name
    prompt = "Write one paragraph on what changed and why it is safe to merge."
  }

  # The graph loops, so bound it here rather than at every invocation.
  max_steps = 12
}
