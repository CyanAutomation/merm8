import type { JevQuestions } from "./types.js";

export const semanticQuestions = {
  "diagram-purpose": {
    type: "choice",
    instructions: "Classify the diagram's intended semantic purpose from its nodes, relationships, labels, diagram type, and supplied structural facts. This is about what the diagram represents, not the Mermaid syntax family.",
    criteria: {
      process: "A sequence of activities, steps, or transformations that achieves an outcome.",
      "decision-tree": "A branching set of conditions or choices whose paths lead to outcomes.",
      architecture: "Components, services, systems, or infrastructure and how they relate.",
      "data-model": "Entities, classes, records, or data concepts and their relationships.",
      interaction: "Messages or interactions between participants over time.",
      "state-machine": "States and transitions that describe lifecycle or reactive behavior.",
      other: "The diagram has another purpose or its purpose cannot be determined from the supplied state.",
    },
  },
  "label-clarity": {
    type: "noul",
    instructions: "Are the node labels sufficiently specific and understandable for a reader to infer what the represented steps or entities mean? Judge the supplied node labels; do not infer detail that is absent.",
    criteria: {
      true: "The labels communicate recognizable steps or entities in this diagram's context. Short labels can be clear when their meaning is apparent.",
      false: "One or more important labels are generic, opaque, or underspecified enough that a reader cannot infer what the represented step or entity means. Do not mark a label unclear merely because it is short.",
    },
  },
  "branch-clarity": {
    type: "noul",
    instructions: "Where the diagram branches, are the alternatives sufficiently distinguishable for a reader to understand why each branch is taken? Use the supplied has-branching fact instead of recounting edges.",
    criteria: {
      true: "There is no branching, or the alternatives at branch points are meaningfully distinguished by their labels or context.",
      false: "The state indicates branching and one or more alternatives are hard to distinguish or their conditions are unclear.",
    },
  },
  "abstraction-consistency": {
    type: "noul",
    instructions: "Are the major nodes expressed at reasonably consistent levels of abstraction? Consider the node labels together and use the diagram purpose as context; a mixture is an observation, not automatically an error.",
    criteria: {
      true: "The major nodes mostly describe concepts at compatible levels, or any variation is understandable for the diagram's purpose.",
      false: "The diagram mixes substantially different levels, such as actors, low-level operations, and infrastructure details, in a way that makes its scope or reading less coherent.",
    },
  },
  ambiguity: {
    type: "noul",
    instructions: "Does the diagram contain materially vague or ambiguous labels or relationships that leave multiple plausible interpretations of its intended meaning? Distinguish this from whether labels are merely concise.",
    criteria: {
      true: "At least one important label or relationship has multiple plausible meanings that could materially change how a reader understands the diagram.",
      false: "The intended meanings and relationships are reasonably determinate, even if some labels are brief or could be more descriptive.",
    },
  },
  "review-priority": {
    type: "choice",
    instructions: "Estimate the priority for a human semantic review based on the supplied diagram state and the semantic judgements requested here. Do not replace deterministic lint severity; assess only the risk of misunderstanding or consequential ambiguity.",
    criteria: {
      none: "The diagram's meaning appears straightforward and no material semantic concern warrants a dedicated review.",
      low: "There are minor clarity or consistency questions, but misunderstanding is unlikely to affect important decisions or use.",
      medium: "A human review would help resolve meaningful uncertainty or inconsistencies before relying on the diagram.",
      high: "Material ambiguity or unclear meaning could lead to consequential misunderstanding, so review should happen before relying on the diagram.",
    },
  },
} satisfies JevQuestions;

export const SEMANTIC_QUESTION_COUNT = Object.keys(semanticQuestions).length;
