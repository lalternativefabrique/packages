package synthesis

import "github.com/lalternativefabrique/cortex/skill"

// Model is the model the synthesis skills run on.
const Model = "mistral-small-3.2-24b-instruct-2506"

var exampleTargets = newSheetTargets(computeSynthesisTargets(400))

var (
	SynthesizeSource = skill.Declare[SynthesizeSourceInput, SynthesisSheet](skill.Spec[SynthesizeSourceInput]{
		ID:           "synthesize_source",
		Name:         "Fiche de compréhension",
		Description:  "Transforme les extraits d'une source en fiche de compréhension : thèse, points clés, axes, enjeux, chaque point cité.",
		Instructions: SynthesizeSourceInstructions,
		Model:        Model,
		Examples: []SynthesizeSourceInput{{
			Language: "fr",
			Targets:  exampleTargets,
			Source:   SheetSource{Type: "article", Title: "Le sommeil et la mémoire"},
			Chunks: []SourceChunk{
				{Ref: "s1", Text: "Pendant le sommeil lent profond, l'hippocampe rejoue les traces de la journée."},
				{Ref: "s2", Text: "Ce rejeu transfère les souvenirs vers le cortex, où ils se consolident."},
			},
		}},
	})
	DigestSourcePart = skill.Declare[DigestSourcePartInput, string](skill.Spec[DigestSourcePartInput]{
		ID:           "digest_source_part",
		Name:         "Condensé d'une partie de source",
		Description:  "Condense une partie d'une source trop longue pour une seule fiche, en gardant les références de ses extraits.",
		Instructions: DigestSourcePartInstructions,
		Model:        Model,
		Examples: []DigestSourcePartInput{{
			Language: "fr",
			Chunks:   []SourceChunk{{Ref: "s91", Text: "Le chapitre 4 décrit la réforme de 1982 et ses effets sur les communes."}},
		}},
	})
	MergeSourceDigests = skill.Declare[MergeSourceDigestsInput, SynthesisSheet](skill.Spec[MergeSourceDigestsInput]{
		ID:           "merge_source_digests",
		Name:         "Fiche d'une source longue",
		Description:  "Fusionne les condensés d'une source longue en une seule fiche de compréhension citée.",
		Instructions: MergeSourceDigestsInstructions,
		Model:        Model,
		Examples: []MergeSourceDigestsInput{{
			Language: "fr",
			Targets:  exampleTargets,
			Source:   SheetSource{Type: "pdf", Title: "Rapport sur la décentralisation"},
			Digests:  []string{"La réforme de 1982 transfère des compétences aux communes [s91][s92].", "Les finances locales dépendent encore de l'État [s180]."},
		}},
	})
)

// Skills are what an agent running the engine must declare in its catalog.
func Skills() []skill.Declared {
	return []skill.Declared{SynthesizeSource, DigestSourcePart, MergeSourceDigests}
}
