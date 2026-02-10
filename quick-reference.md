# Quick Reference

Canvas: **900×550px**. Origin (0,0) = top-left.

## Essential Instructions

```
moveTo(x, y)                           — move without drawing
lineTo(x, y)                           — draw line to point
drawRect(x, y, width, height, fill?)   — rectangle
drawCircle(x, y, radius, fill?)        — circle
writeText(text, x, y, fontSize?)       — text at position
setColor(color)                        — stroke color (e.g., "#2196F3")
```

## JSON Format

Every instruction is an object with `"type"` plus type-specific params:
```json
{"type": "drawRect", "x": 100, "y": 100, "width": 200, "height": 80, "fill": "#E3F2FD"}
{"type": "writeText", "text": "Hello", "x": 150, "y": 150, "fontSize": 16}
{"type": "moveTo", "x": 300, "y": 140}
{"type": "lineTo", "x": 400, "y": 140}
{"type": "setColor", "color": "#4CAF50"}
```

## Drawing Arrows (no arrow primitive)

Rightward arrow ending at (ex, ey):
```json
{"type": "lineTo", "x": ex, "y": ey},
{"type": "moveTo", "x": ex, "y": ey},
{"type": "lineTo", "x": ex-10, "y": ey-6},
{"type": "moveTo", "x": ex, "y": ey},
{"type": "lineTo", "x": ex-10, "y": ey+6}
```

Leftward: `ex+10` instead of `ex-10`. Downward: swap x/y offsets.

## Colors

| Purpose | Stroke | Fill |
|---------|--------|------|
| Primary (blue) | #2196F3 | #E3F2FD |
| Success (green) | #4CAF50 | #E8F5E9 |
| Warning (orange) | #FF9800 | #FFF3E0 |
| Error (red) | #F44336 | #FFEBEE |
| Annotation | #666666 | — |

## Layout Tips

- Leave 30px margins → work within 840×490
- Font: 14-16 body, 18+ titles, 11-12 annotations
- One concept per diagram; use send_message for text explanations
