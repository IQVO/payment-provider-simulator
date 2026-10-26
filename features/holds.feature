Feature: Capturing, releasing and refunding a hold

  Background:
    Given a token for scenario "approve"
    And a hold of 1000 EUR with that token

  Scenario: Partial captures never exceed the hold
    When I capture 600
    Then the response status is 201
    When I capture 401
    Then the response status is 409
    And the problem type is "over-capture"

  Scenario: Refunds never exceed what was captured
    Given I capture 500
    When I refund 501
    Then the response status is 409
    And the problem type is "over-refund"
    When I refund 500
    Then the response status is 201

  Scenario: Reduce releases part of the remaining hold
    Given I capture 200
    When I reduce the hold to 300
    Then the response status is 200
    And the hold shows remaining 300
    When I reduce the hold to 301
    Then the response status is 422
    And the problem type is "not-a-reduction"

  Scenario: A voided hold accepts no capture
    When I void the hold
    Then the response status is 200
    When I capture 1
    Then the response status is 409
    And the problem type is "illegal-transition"

  Scenario: Reauthorizing replaces the hold with a fresh one
    When I reauthorize the hold for 800
    Then the response status is 201
    And the old hold is "SUPERSEDED"

  Scenario: An unknown hold is a 404
    When I capture 1 on the hold "hold_missing"
    Then the response status is 404
    And the problem type is "not-found"
