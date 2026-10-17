# Security

## Reporting a problem

If you find a security problem in rflib, such as a way past sign-in, into another person's library, or onto the server, please report it privately rather than in a public issue: on the repository's **Security** tab, choose **Report a vulnerability**. That opens a private report that only the maintainer, Ryan Geyer (W6FAM), sees.

Say what you found, how to reproduce it, which rflib (`rflib version`), and how it was run (the command line, or `rflib serve`, with or without `--accounts`). You'll get a reply, and credit in the fix's release notes unless you'd rather not.

## What's covered

The latest release, and the `main` branch. rflib is in public preview, so fixes go into the next release rather than older ones.

The parts that matter most are those facing a network: `rflib serve`, its sign-in and sessions (see [Accounts and hosting](docs/accounts.md)), and the HTTP API. Some trade-offs are deliberate and documented: `--plain-http` sends passwords unencrypted, for networks such as AREDN where HTTPS isn't possible, and anyone with the token of a server without accounts can change its library.
