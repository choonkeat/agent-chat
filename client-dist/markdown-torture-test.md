# Markdown Torture Test

Use this to verify the chat's markdown renderer covers all supported features.

## Inline Formatting

This has **bold text** and *italic text* and `inline code` and **bold with *nested italic* inside** and a [link to Google](https://www.google.com).

Here's a bare URL: https://example.com/path?query=1&foo=bar

And __underscore bold__ plus _underscore italic_ plus some_snake_case_words that should NOT be italic.

---

## Blockquotes

> This is a simple blockquote with **bold** and *italic* text.
> It spans multiple lines.

> Here's a quote with `inline code` and a [link](https://example.com).

Nested quotes:

>> This is a double-nested quote.
>> It should appear indented twice.

> Back to single depth.
> With a second line.

---

## Lists

Unordered:
- First item with **bold**
- Second item with `code`
- Third item with a [link](https://example.com)

Ordered:
1. Step one
2. Step two
3. Step three with *emphasis*

## Code Blocks

```javascript
function greet(name) {
  const msg = `Hello, ${name}!`;
  console.log(msg);
  return msg;
}
```

```python
def fibonacci(n):
    """Generate fibonacci sequence."""
    a, b = 0, 1
    for _ in range(n):
        yield a
        a, b = b, a + b

print(list(fibonacci(10)))
```

```go
func main() {
    ch := make(chan int, 10)
    go func() {
        for i := 0; i < 10; i++ {
            ch <- i * i
        }
        close(ch)
    }()
    for v := range ch {
        fmt.Println(v)
    }
}
```

## Table

| Feature | Status | Notes |
|---------|:------:|------:|
| Bold | Done | **works** |
| Italic | Done | *works* |
| Code | Done | `works` |
| Links | Done | [click](https://example.com) |
| Tables | Done | you're looking at one |

## Headings

### H3 heading
#### H4 heading
##### H5 heading
###### H6 heading

---

## Edge Cases

Empty bold: **** and empty italic: ** and empty code: ``

Multiple paragraphs separated by blank lines.

This is paragraph two.

A line with `code containing **asterisks** and [brackets]` inside.

***

End of torture test.
