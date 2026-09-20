# How it works

It might be useful to understand the internal mechanics of it, because certain behavior or usage limitations will be then more obvious.

Once you specify one or more logstreams on the query edit form, and submit it, Nerdlog will initiate a separate ssh connection for every logstream (except for `localhost`).  If we have multiple logstreams on the same host, as of today it'll still make a separate ssh connection for every logstream.

Then, for every logstream:

  * Once connected to the host, it'll upload an agent bash script under `/tmp` on the host (that agent script will be facilitating the querying later on);
  * Invoke it right away to check some details about the host, such as the timezone, a few example log lines to detect the timestamp format, and awk version;
  * If everything is alright, execute the first query, printing results to stdout and stderr (which Nerdlog reads), and keep the connection mostly idle until the user submits the next query.

## Overview of query implementation

Here's how a query is executed, on a high level. Conceptually, here are the steps that we need to take:

First, on the agent side:

  * Cut the parts of the logs outside of the requested time range; this is done using `tail` and/or `head` and with the help of an index file (see below);
  * On the remaining part, only keep the lines which match the provided awk pattern. Effectively, if we have a non-empty pattern such as `/foo/`, then the awk script will have this line: `!(/foo/) {next}`. So far, no effort is made to sanitize the input, so it's possible to do "awk injections" if one wants to, but by doing so the user would only hurt themselves (since they have ssh access to the host, and can do anything in the first place).
  * For the remaining lines:
    * Generate data for the timeline histogram: basically a mapping from the minute to the number of log lines that happened during that minute, and print it to stdout;
    * Print the latest N log lines to stdout, in the raw form exactly as they are present in the log file(s).

Additionally, the agent prints some progress info to stderr, such that Nerdlog can show it on the UI, and we know how far we are in the query. Very convenient for large log files, especially when the index file is being generated (see details below).

And on the Nerdlog side:

  * Wait for the agents on all the logstreams to return the aforementioned data (timeline histogram data + some latest log lines);
  * Merge them together;
  * Parse the log messages, so that instead of the raw messages, we'll have a `time` and potentially some other parts factored out as separate columns in the UI table. For syslog messages, it means having fields such as `hostname`, `program` and `pid`. Ideally, this part should also be done by a user-provided Lua script, to be able to parse some app-specific formats as well; but for now this kind of scripting is TODO. Also, every log message has a built-in field `lstream`, containing the name of the logstream it's coming from. Unlike fields parsed from the log record, its value is stored directly in the log message rather than in the parsed context.
  * Obviously, render everything on the UI.

An important point here is that, perhaps unintuitively, the awk pattern is checked against *raw log lines*, which might not be exactly what we see in Nerdlog UI. So for example, if in the UI we see a column `program` being `foo`, and want to filter logs only with that value of `program`, when writing an awk pattern we have to think how it looks in the raw log file. Perhaps just `/foo/` can be good enough, but keep in mind that it'll potentially match more logs (those that contain `foo` in some other place, not necessarily in the `program` field)

## Index file

As mentioned above, the first step when executing a query is cutting the logs outside of the requested time range. It could be done by manually checking every line in a logfile to find the right place, but if the log files are large and the timerange being queried is relatively small (which is often the case), this is the slowest part of the query and it's often repeated in multiple subsequent queries.

So to optimize that, the agent script maintains an index file: basically a file stored as `/tmp/nerdlog_agent_index_.....`, with a mapping from a timestamp like `2025-03-09-06:02` to the line number and byte offset in the corresponding log file. As you see, the resolution here is 1 minute; it means we can't query time ranges more granular than a minute.

So when a query comes in, with the starting timestamp being e.g.  `2025-04-20-09:05`, the agent first checks if the index file already has this timestamp. If so, then we know which part of the file to cut. If not, and the requested timestamp is later than the last one in the index, we need to "index up": add more lines to the index file, starting from the last one there. And obviously there's logic to invalidate index files and regenerate them from scratch; this happens when log files are being rotated.

So indexing does take some time (on 2GB log file it takes about 10s in my experiments), but it only has to be done once after the log files were rotated, so at most once a day in most setups. And thanks to that, the timerange-based part of the query is very efficient: we know almost right away which parts of the log files to cut.
