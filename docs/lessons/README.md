# Go lessons

Notes written while building `quantic-cli`, by someone whose daily languages are Elixir and Ruby. The
same approach as [quantic-agent-go's lessons](https://github.com/fleveque/quantic-agent-go/tree/main/docs/lessons),
on a program Go is the usual choice for.

Each lesson pairs with a milestone from the [design](../design.md#12-milestones). They're not a Go
tutorial: they're the things that surprised me, the assumptions I carried over from other languages
that turned out wrong, and the reasoning behind Go's choices where I found it non-obvious.

| # | Lesson | Milestone | Read online | Code walkthrough |
|---|---|---|---|---|
| [01](01-commands-flags-and-exit-codes.md) | Commands, flags, and exit codes | 1 — the command skeleton | [Commands, flags, and exit codes](https://claude.ai/artifact/XWB84F2VbghTJFbE7GaB7S) | [The command skeleton, line by line](https://claude.ai/artifact/PpMKhtpWX1LLqWtWpuXZDG) |
| [02](02-a-generated-client-and-the-network.md) | A generated client, and the network | 2 — the generated client | [A generated client, and the network](https://claude.ai/artifact/7kkCVCorLUBF6cjJFduzb5) | [The generated client, line by line](https://claude.ai/artifact/RZgW6D3BNvvxrqLVpW6jCZ) |
| [03](03-a-token-the-keyring-and-your-own-data.md) | A token, the keyring, and your own data | 3 — signing in | [A token, the keyring, and your own data](https://claude.ai/artifact/AKBiEL9M7YYDTGskQD8md9) | [Signing in, line by line](https://claude.ai/artifact/2SYakbzwsUHAzUKasqxrre) |
| [04](04-a-cache-that-keeps-the-bar-alive.md) | A cache that keeps the bar alive | 4 — the cache | [A cache that keeps the bar alive](https://claude.ai/artifact/8aYsD9oBMfykrCjLAb2FwZ) | [The cache, line by line](https://claude.ai/artifact/VaoAQndrkGKbrypASjjvZX) |
| [05](05-three-calls-at-once.md) | Three calls at once | 5 — `upcoming` and `status` | [Three calls at once](https://claude.ai/artifact/VpBiP5EfMBe9DqRJbByqpo) | [Status, line by line](https://claude.ai/artifact/BtpJFr2EwPNmWyJdJGny9h) |
| [06](06-the-other-side-of-the-contract.md) | The other side of the contract | 6 — the Omarchy widget | [The other side of the contract](https://claude.ai/artifact/2DyPcTgPY5PeZsMaDVVwGH) | — (no Go; the code is [omarchy-quantic](https://github.com/fleveque/omarchy-quantic)) |
| [07](07-a-release-is-a-tag.md) | A release is a tag | 7 — releases | [A release is a tag](https://claude.ai/artifact/KFpakZA3KQHZu5cV8pDgfq) | [The release, line by line](https://claude.ai/artifact/9ZCcg66Ja5iLtYb394q2jw) |

Lessons tell the story of a milestone: what surprised me and why. Walkthroughs go through the code
itself in reading order, the way a tech lead would with a new teammate. Server milestones (S1, S2)
are Elixir work in Quantic and have no lesson here.

More land as the milestones do.
