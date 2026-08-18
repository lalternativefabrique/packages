Feature: Keeping what the author wrote
  As someone writing a post
  I want my own text kept and every version readable
  So that adapting it for a platform never costs me what I wrote

  Background:
    Given an author writing a composition

  Scenario: Adapting for a platform leaves the author's text alone
    Given the author wrote "ce que j'ai écrit moi-même"
    When the composition is adapted for "linkedin" as "une version plus punchy"
    Then the author's text is still "ce que j'ai écrit moi-même"
    And the "linkedin" variant reads "une version plus punchy"

  Scenario: Adapting one platform leaves the others alone
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "pour linkedin"
    When the composition is adapted for "bluesky" as "pour bluesky"
    Then the "linkedin" variant reads "pour linkedin"
    And the "bluesky" variant reads "pour bluesky"

  Scenario: Writing an article does not replace the post
    Given the author wrote "mes notes brutes"
    When the composition is adapted for "devto" as "# Un article long"
    Then the author's text is still "mes notes brutes"

  Scenario: Every draft stays readable
    Given the author wrote "premier jet"
    And the author paused for 5 minutes
    And the author wrote "deuxième jet"
    And the author paused for 5 minutes
    And the author wrote "troisième jet"
    Then the composition has 3 source versions
    And source version 1 reads "premier jet"
    And source version 2 reads "deuxième jet"

  Scenario: A sitting reads as one version, not as one per keystroke
    Given the author wrote "d apres grams"
    And the author wrote "d apres gramshy on devrait"
    And the author wrote "d apres gramshy on devrait avoir une lutte des classes"
    Then the composition has 1 source versions
    And source version 1 reads "d apres gramshy on devrait avoir une lutte des classes"

  Scenario: Coming back to a text later starts a new version
    Given the author wrote "ce que j'ai écrit ce matin"
    And the author paused for 5 minutes
    And the author wrote "ce que j'ai ajouté ce soir"
    Then the composition has 2 source versions
    And source version 1 reads "ce que j'ai écrit ce matin"

  Scenario: Writing a paragraph is a step, even without stopping
    Given the author wrote "un premier jet"
    And the author wrote "un premier jet, puis une phrase entière ajoutée juste après sans jamais s'arrêter de taper, assez longue pour compter comme une étape à part entière du texte"
    And the author wrote "un premier jet, puis une phrase entière ajoutée juste après sans jamais s'arrêter de taper, assez longue pour compter comme une étape à part entière du texte, et un mot de plus"
    Then the composition has 2 source versions

  Scenario: Cutting a paragraph is a step too
    Given the author wrote "un texte long, avec une phrase entière qui sera coupée juste après sans jamais marquer la moindre pause, assez longue pour que sa disparition compte comme une étape à part entière"
    And the author wrote "un texte long"
    Then the composition has 2 source versions

  Scenario: Fixing a few words is not a step
    Given the author wrote "un texte avec une faute de frape et deux mots à revoir"
    And the author wrote "un texte avec une faute de frappe et deux mots à revoir"
    And the author wrote "un texte avec une faute de frappe et deux mots revus"
    Then the composition has 1 source versions
    And source version 1 reads "un texte avec une faute de frappe et deux mots revus"

  Scenario: Replaying an earlier version returns the text as it was
    Given the author wrote "ce que j'avais écrit"
    And the author paused for 5 minutes
    And the author wrote "ce que j'ai écrit ensuite"
    When the composition is replayed at its first source version
    Then the replayed text is "ce que j'avais écrit"

  Scenario: A variant keeps its own history
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "première adaptation"
    And the author paused for 5 minutes
    When the composition is adapted for "linkedin" as "seconde adaptation"
    Then the "linkedin" variant has 2 versions
    And the "linkedin" variant reads "seconde adaptation"

  Scenario: An unchanged autosave records nothing
    Given the author wrote "un texte stable"
    When the author saves the same text again
    Then the composition has 1 source versions

  Scenario: A revised passage is a step of its own
    Given the author wrote "un texte à retoucher"
    When the author revised a passage into "un texte retouché"
    Then the composition has 2 source versions
    And source version 1 reads "un texte à retoucher"

  Scenario: Each dictation is its own version
    Given the author wrote "une première note"
    When the author dictated "une première note, et ce que je viens de dire"
    And the author dictated "une première note, et ce que je viens de dire, puis autre chose"
    Then the composition has 3 source versions
    And source version 2 reads "une première note, et ce que je viens de dire"

  Scenario: Placing an illustration is a step of its own
    Given the author wrote "un texte à illustrer"
    When the author illustrated "un texte à illustrer, avec une image"
    Then the composition has 2 source versions
    And source version 1 reads "un texte à illustrer"

  Scenario: The picture arriving is not a step the author took
    Given the author wrote "un texte à illustrer"
    And the author illustrated "un texte à illustrer, avec une image"
    When the picture settled into "un texte à illustrer, avec une image rendue"
    Then the composition has 2 source versions
    And source version 2 reads "un texte à illustrer, avec une image rendue"

  Scenario: Restoring right after writing keeps the text it replaced readable
    Given the author wrote "ce que j'avais écrit"
    And the author paused for 5 minutes
    And the author wrote "ce que je viens d'écrire"
    When the author restores "ce que j'avais écrit"
    Then the composition has 3 source versions
    And source version 2 reads "ce que je viens d'écrire"

  Scenario: Two documents differing past what a float can hold are not the same
    Given the author wrote "un texte" with rich content '{"id":12345678901234567890}'
    And the author paused for 5 minutes
    When the author saves the same text with rich content '{"id":12345678901234567891}'
    Then the composition has 2 source versions

  Scenario: The same document spelled differently is not a new version
    Given the author wrote "un texte stable" with rich content '{"type":"doc","content":[]}'
    When the author saves the same text with rich content '{"content":[],"type":"doc"}'
    Then the composition has 1 source versions

  Scenario: A correction is never folded into the generation it corrects
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "ce que le modèle a écrit"
    When the author corrects the "linkedin" variant to "ce que j'ai corrigé"
    Then the "linkedin" variant has 2 versions
    And the last "linkedin" version is an author edit

  Scenario: Successive corrections in one sitting read as one
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "ce que le modèle a écrit"
    When the author corrects the "linkedin" variant to "ma correction"
    And the author corrects the "linkedin" variant to "ma correction, affinée"
    Then the "linkedin" variant has 2 versions
    And the "linkedin" variant reads "ma correction, affinée"

  Scenario: A variant goes stale when the author rewrites their text
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "adapté du texte source"
    When the author wrote "le texte source, retravaillé"
    Then the "linkedin" variant is stale

  Scenario: A variant cannot be derived from an empty source
    When the composition is adapted for "linkedin" as "quelque chose"
    Then the adaptation is refused

  Scenario: The stream is what holds the history
    Given the author wrote "un texte qui doit survivre"
    And the composition is adapted for "linkedin" as "son adaptation"
    When the composition is loaded again from the event store
    Then the author's text is still "un texte qui doit survivre"
    And the "linkedin" variant reads "son adaptation"

  Scenario: A failed adaptation says why
    Given the author wrote "le texte source"
    When adapting for "linkedin" fails with "quota exceeded"
    Then the "linkedin" variant is failed
    And the "linkedin" variant reason is "quota exceeded"

  Scenario: A requested adaptation shows as generating
    Given the author wrote "le texte source"
    When an adaptation is requested for "linkedin"
    Then the "linkedin" variant is generating

  Scenario: An author's correction is its own kind of version
    Given the author wrote "le texte source"
    And the composition is adapted for "linkedin" as "ce que le modèle a écrit"
    When the author corrects the "linkedin" variant to "ce que j'ai corrigé"
    Then the "linkedin" variant has 2 versions
    And the last "linkedin" version is an author edit
