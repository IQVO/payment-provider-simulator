Feature: Hold expiry

  Scenario: An expire-early hold cannot be captured once it expired
    Given a token for scenario "expire-early"
    And a hold of 1000 EUR with that token
    When 6 minutes pass
    And I capture 1
    Then the response status is 409
    And the problem type is "hold-expired"

  Scenario: Reauthorizing before expiry keeps the money capturable
    Given a token for scenario "expire-early"
    And a hold of 1000 EUR with that token
    When 4 minutes pass
    And I reauthorize the hold for 1000
    And 3 minutes pass
    And I capture 1000 on the new hold
    Then the response status is 201
