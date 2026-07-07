import os
import re

def process_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()

    original = content

    if filepath.endswith('moqt/frame.go'):
        content = content.replace(
            'header, _, _ := message.WriteMessageLength(f.header[:0], l)',
            'header, _, err := message.WriteMessageLength(f.header[:0], l)\n\tif err != nil {\n\t\treturn err\n\t}'
        )

    if original != content:
        with open(filepath, 'w') as f:
            f.write(content)

process_file('moqt/frame.go')
