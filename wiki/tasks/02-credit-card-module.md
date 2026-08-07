# Credit Card Module

This module will be responsible for managing user's credit cards. It will have commands for create, list, details, edit, delete. All command will have shortcuts.

## Commands

list: "kakei credit-card" or "kakei cc"
create: "kakei credit-card new" or "kakei cc n"
edit: "kakei credit-card edit {CODE|ID?}" or "kakei cc e {CODE|ID?}"
delete: "kakei credit-card delete {CODE|ID?}" or "kakei cc d {CODE|ID?}"
details: "kakei credit-card {CODE|ID?}" or "kakei cc {CODE|ID?}"

## Suggested structure

name: string
description?: string
code: string (length 5)
color: string
limit: int
balance: int
currencyType: string
closingDate: datetime
dueDate: datetime

also basic columns

## Rules
- The color must be a pre-set of 12 colors to easily identify the accounts
- code is required and must have a random suggestion for the user
- all commands that uses CODE|ID will shows an account select if no code is provided
- it must use the bubbles and lipgloss package to create a great UX
- there will be 4 options as currencyType: Dollar, Euro, Brazilian Real, Bitcoin
- if the commands is used if the flag "-h" or "--help" it will show a small documentation of that command only

## Notes

Some rules will be add later when transactions module is created.

