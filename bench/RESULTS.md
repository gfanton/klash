# Results

Measured in November 2026 with the harness in this folder: gnosh against
Charm's crush (v0.100.0, the project gnosh is forked from) and OpenAI's
Codex CLI (0.167.0), each run on the same tasks with the same model on the
same server. The four tasks are in [tasks/](tasks/README.md): rename a
project, implement nine small packages (algos), fix six bugs, and a job
split over two sessions (continue).

## 1. The swarm: several gnosh sessions on one plan

A model server with memory to spare runs several requests at once for
little more than the cost of one. On a rented RTX 6000 Ada (48 GB) serving
Qwen 3.8 27B (FP8) with vLLM, `streams.py` measured:

| requests at once | tokens/s, all together | tokens/s, each |
|---|---|---|
| 1 | 30 | 30 |
| 2 | 57 | 28 |
| 3 | 84 | 28 |
| 4 | 111 | 28 |
| 6 | 167 | 28 |
| 8 | 218 | 27 |

A single coding session uses one of those streams. gnosh sessions in one
project share one mem, so several of them can work one plan: each takes a
free todo under mem's lock, and none takes a todo twice. On the same
server, each harness alone on it in turn, with the server's reasoning
effort at medium for all of them:

**algos**: nine packages to write, a test suite each. Every run passed 9/9.

| harness | sessions | each run, in seconds | median | model requests a run |
|---|---|---|---|---|
| gnosh swarm | 6 | 312, 1,485¹, 409, 549², 704, 500, 524, 401, 275 | 500 s | 95–129 |
| crush | 1 | 550, 461, 435, 410 | **448 s** | 11–13 |
| gnosh swarm | 3 | 692, 554 | 623 s | 105–107 |
| gnosh | 1 | 850, 713, 738, 1,405 | 794 s | 23–74 |
| Codex | 1 | 1,240, 628 | 934 s | 7–21 |

gnosh's rounds ran on three builds of main, as the fixes below landed:
`d06172a` (the six sessions' rounds 1–4, one session's 1–2), `456ff18`
(rounds 5–6) and `818c7bc` (rounds 7–9, one session's 3–4).

¹ The planner put `.Parallel()` on each of its nine todos instead of on
the todo they were under, where the mark does nothing, so the plan ran in
single file: the planner did all nine while the other five sessions waited
twelve minutes in `mem.Next()`. The other twelve planners of the night
put the mark on the plan. gnosh now refuses such a plan and says where
the mark goes (`456ff18`).

² A session asked `mem.Next()` for its next todo and, in the same reply,
opened the files of the one it guessed came next. Next gave it another; it
worked the guessed one, which another session held, and closed the one it
had with that work. The close review let it through, the package stayed
unwritten until the plan's last check, and the planner wrote it alone
after the rest had finished. Replayed, the old review passed that close in
5 tries of 10; asked first whether the result is the todo's own, it
refused it in 10 of 10 and passed the seven good closes replayed beside it
(`818c7bc`, `31868d6`). gnosh also tells a session now to call
`mem.Next()` by itself.

What the rounds show:

- **Six sessions finish a wide job well ahead of one gnosh session**: 500 s
  against 794 s at the median, 1.6 times faster. Three sessions gained less
  (623 s).
- **Six gnosh sessions and crush come out about even.** crush's median is
  448 s; the six sessions' is 500 s over all nine rounds, and 401 s over the
  three on the last build. The fastest run of the night was the swarm's
  (275 s), and so was the slowest. crush does well alone because it
  batches: its busiest replies made five or six tool calls, writing several
  packages at once, where a gnosh session takes one step a reply (enter a
  todo, write it, test it, pop it). A six-session run made about 110 model
  requests to crush's 13 and generated almost three times the tokens
  (medians 34,772 and 12,296), each reply with its own reasoning; six
  streams at once hide most of that, not all.
- **A swarm runs as long as its slowest session.** In rounds 6 and 7 the
  others had finished minutes before the last one, which had spent 3½
  minutes on a single reply of about 4,700 tokens for the hardest package,
  topo: in round 6 thinking 14,000 characters before it opened the file,
  in round 7 writing a program in Gno to check the algorithm before the Go.
  One session alone varies as much: its fourth round wrote 36,942 tokens
  where its first wrote 22,381, and took 1,405 s.
- **The two slowest swarm runs each found a flaw in gnosh**, fixed since (¹
  and ²). On the last build the review refused none of the 47 closes it
  checked.

**bugs**: six bugs in a small shop module, each found by a failing test.
Every run passed.

| harness | sessions | each run, in seconds | median |
|---|---|---|---|
| crush | 1 | 139, 119 | **129 s** |
| gnosh swarm | 6 | 182, 209 | 196 s |
| gnosh | 1 | 223, 405 | 314 s |
| gnosh before fix A (`5b9ce3e`) | 1 | 791, 686 | 738 s |

Six one-line fixes take crush two minutes, and on a job that small the
swarm's plan, its calls to mem and the review of each close cost more than
six sessions save. The last row is gnosh before a todo's evidence counted
from where its plan was made (section 3): 4 and 9 of its calls to mem were
refused at a close, against 1 and 0 since.

## 2. One session each, before the swarm

**Qwen 3.8 27B on a 128 GB home AI box** (llama.cpp, 64K window, two
rounds): both gnosh and crush passed all four tasks; crush was 1.3x to 4x
faster on every one. Most of gnosh's extra time was spent on todo closes
that its close check refused: the model did the work first and closed the
todos after, and the check only counted work done inside each todo. On the
bugs task every test was green at 684 s and gnosh closed todos until
1,667 s, where crush was done in 443 s.

**Non-Qwen models on an H100** (vLLM, 64K window; gpt-oss-120b three
rounds, GLM-4.7-Flash and Gemma 4 31B two):

| model | gnosh passed | crush passed | Codex passed | median time per task, gnosh / crush / Codex | fresh tokens read, gnosh / crush / Codex |
|---|---|---|---|---|---|
| gpt-oss-120b | 10/12 | 9/12 | 5/12 | 84 / 88 / 76 s | 23K / 27K / 18K |
| GLM-4.7-Flash | 6/8 | 5/8 | 3/8 | 211 / 168 / 194 s | 47K / 20K / 19K |
| Gemma 4 31B | 7/8 | 8/8 | 5/8 | 208 / 162 / 221 s | 84K / 27K / 20K |
| all three | 23/28 | 22/28 | 13/28 | | |

What these showed, besides the refused closes:
- gpt-oss mostly skipped mem (4 runs of 12 used it); GLM and Gemma used
  it in nearly every run.
- On GLM and Gemma, a reminder gnosh put into the conversation as a user
  message made the server read the whole turn again: their chat templates
  drop earlier reasoning once a user message arrives (`cachetest.py`: one
  more step cost 121 and 227 tokens of re-reading, the reminder 16,746 and
  19,727). gpt-oss and Qwen 3.8 were not affected (32 tokens).
- Codex offers no patch tool for a model outside its own catalog, and vLLM
  rejects its patch tool anyway, so on these servers it edited through its
  shell; most of its failures come from that.

## 3. What changed in gnosh since

- **A todo's evidence counts from where its plan was made** (main
  `fb35b81`). A model that works a whole plan first and then closes each
  todo is no longer refused for doing the work before entering the todo;
  the close is still checked against that work, so a todo closed before
  its work was done is still refused.
- **The close review takes a view or a passing test of a change made
  before** (main `d06172a`).
- **A plan that marks each of its todos `.Parallel()` is refused**, and
  told to mark the todo they are under (main `456ff18`, footnote ¹ above).
- **A close is judged first on whether its result is the todo's own**, and
  a session is told to call `mem.Next()` by itself (main `818c7bc` and
  `31868d6`, footnote ² above).
