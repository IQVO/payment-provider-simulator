Feature: Tokens and authorizations
  The simulator is the stand-in for the external payment provider. A payment-method
  token names its test scenario, so every outcome is deterministic.

  Scenario: An approve token authorizes a hold that lasts seven days
    Given a token for scenario "approve"
    When I authorize 1000 EUR with that token
    Then the response status is 201
    And the hold expires in 10080 minutes

  Scenario: A decline token is a final 402 with a decline code
    Given a token for scenario "decline"
    When I authorize 1000 EUR with that token
    Then the response status is 402
    And the problem type is "payment-declined"
    And the decline code is "CARD_DECLINED"

  Scenario: An expire-early token produces a hold that expires in minutes
    Given a token for scenario "expire-early"
    When I authorize 1000 EUR with that token
    Then the response status is 201
    And the hold expires in 5 minutes

  Scenario: A timeout token answers 504 after a short delay
    Given a token for scenario "timeout"
    When I authorize 1000 EUR with that token
    Then the response status is 504
    And the problem type is "provider-timeout"

  Scenario: The same Idempotency-Key returns the same hold
    Given a token for scenario "approve"
    When I authorize 1000 EUR with that token under key "k-1"
    And I authorize 1000 EUR with that token under key "k-1"
    Then both answers carry the same hold id

  Scenario: A key reused with a different request is refused
    Given a token for scenario "approve"
    When I authorize 1000 EUR with that token under key "k-1"
    And I authorize 2000 EUR with that token under key "k-1"
    Then the response status is 422
    And the problem type is "idempotency-key-reused"

  Scenario: A card number is never accepted
    When I authorize 1000 EUR with the token "4111111111111111"
    Then the response status is 422
    And the problem type is "validation-failed"
