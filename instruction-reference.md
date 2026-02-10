# Drawing Instruction Reference

All instructions are JSON objects with a `type` field plus type-specific parameters.
Each `draw` call creates a new canvas bubble (900×550 pixels) in the chat.

## Movement
| type | params | description |
|------|--------|-------------|
| moveTo | x, y | Move to absolute position (no drawing) |
| lineTo | x, y | Draw a line from current position to (x, y) |

## Style
| type | params | description |
|------|--------|-------------|
| setColor | color | Set stroke color (CSS color string, e.g. "#ff0000") |
| setStrokeWidth | width | Set stroke width in pixels |

## Shapes
| type | params | description |
|------|--------|-------------|
| drawRect | x, y, width, height, fill?, fillStyle? | Draw rectangle (fill is optional CSS color) |
| drawCircle | x, y, radius, fill?, fillStyle? | Draw circle |
| drawEllipse | x, y, width, height, fill?, fillStyle? | Draw ellipse |

**fillStyle** (optional, default "solid"): "solid", "hachure", "zigzag", "cross-hatch", "dots", "dashed", "zigzag-line"

## Text
| type | params | description |
|------|--------|-------------|
| writeText | text, x, y, fontSize?, font? | Draw text at (x, y) where y is vertical center of text |
| label | text, offsetX?, offsetY?, fontSize? | Draw text near current turtle position |

**Text centering:** The y coordinate specifies the vertical center of the text. To center text in a box at (bx, by, width, height), use y = by + height/2.

## Control
| type | params | description |
|------|--------|-------------|
| clear | *(none)* | Clear the canvas |
| wait | duration | Pause animation for duration milliseconds |

## Canvas
Default canvas size is **900 × 550** pixels. Origin (0,0) is top-left.
