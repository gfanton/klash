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
server, each harness alone on it in turn, on algos (nine packages to
write, a test suite each; every run below passed 9/9):

| harness | sessions | round 1 | model requests | tokens read / generated |
|---|---|---|---|---|
| gnosh swarm | 6 | **312 s** | 108 | 175K / 27K |
| crush | 1 | 550 s | 13 | 46K / 15K |
| gnosh swarm | 3 | 692 s | 107 | 215K / 41K |
| gnosh | 1 | 850 s | 30 | 109K / 22K |
| Codex | 1 | 1,240 s | 21 | 66K / 35K |

Six gnosh sessions finished 1.8x faster than crush and 2.7x faster than one
gnosh session. The first session had its plan (ten todos under
`.Parallel()`) in mem at 26 s; the other five started then, and all six
ended between 211 s and 300 s. The swarm does more work in all (108
requests against crush's 13), and on a server with memory to spare that
work runs side by side for almost nothing. crush does well alone because
it batches: its busiest replies made 5 or 6 tool calls, writing several
packages at once, where a gnosh session takes one step per reply (enter a
todo, write it, test it, pop it). With three sessions that step-by-step
work still took longer than crush's single batched stream; with six it
did not.

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
