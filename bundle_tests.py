#!/usr/bin/python3

import os
from pathlib import Path

def bundle_test_programs(output_file_path="bundled_tests.txt"):
    # Target directory relative to where the script is run
    base_dir = Path("test/programs")
    
    if not base_dir.exists():
        print(f"Error: The directory '{base_dir}' does not exist.")
        return

    print(f"Scanning {base_dir} and writing to {output_file_path}...")

    with open(output_file_path, "w", encoding="utf-8") as out_file:
        # Iterate through every item inside test/programs
        for subdir in sorted(base_dir.iterdir()):
            if subdir.is_dir():
                # out_file.write(f"{'='*80}\n")
                # out_file.write(f"DIRECTORY: {subdir.name}\n")
                # out_file.write(f"{'='*80}\n\n")

                # Define the files we want to capture
                # You can add '.exitcode.txt' or '.expected.txt' to this list if needed
                target_extensions = [
                    ".maml", 
                    # ".tast.json",
                    # ".mir.json"
                ]

                for ext in target_extensions:
                    # Find files with the specific extension in the subdirectory
                    for file_path in sorted(subdir.glob(f"*{ext}")):
                        out_file.write(f"--- START FILENAME: {file_path.name} ---\n")
                        
                        try:
                            content = file_path.read_text(encoding="utf-8")
                            out_file.write(content)
                        except Exception as e:
                            out_file.write(f"[ERROR READING FILE: {e}]\n")
                            
                        out_file.write(f"\n--- END FILENAME: {file_path.name} ---\n\n")
                
                out_file.write("\n")

    print("Bundling complete!")

if __name__ == "__main__":
    bundle_test_programs()
