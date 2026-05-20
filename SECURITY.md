# Security Policy

`osrs-ge` is intended to be read-only research tooling.

## Do Not Share Secrets

Do not put account credentials, game credentials, API tokens, session cookies,
wallet keys, SSH keys, or other secrets into issues, logs, examples, screenshots,
or local SQLite files.

## Supported Reports

Please report issues related to:

- accidental credential exposure
- unintended filesystem writes
- unsafe command execution
- dependency or supply-chain risk
- behavior that could enable client automation or gameplay automation

## Out Of Scope

The project does not place trades, control clients, or authenticate to OSRS
accounts. Requests to add those behaviors are outside project scope.

