#!/usr/bin/env python3
"""Compare two QDC507 full-NAND images without exposing their contents.

The tool reads the MIBIB partition table from each image. It reports aggregate
byte, page, and erase-block differences for each partition. It also compares
UBI logical erase blocks when a partition contains UBI erase-counter headers.

The output does not include payload bytes, extracted strings, or image hashes.
"""

from __future__ import annotations

import argparse
import hashlib
import struct
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


MIBIB_MAGIC = struct.pack("<II", 0x55EE73AA, 0xE35EBDDB)
MIBIB_HEADER = struct.Struct("<IIII")
MIBIB_ENTRY = struct.Struct("<16sIII")
UBI_LAYOUT_VOLUME_ID = 0x7FFFEFFF


class AnalysisError(Exception):
    """Report an input or image-layout error."""


@dataclass(frozen=True)
class Geometry:
    page_size: int
    pages_per_block: int

    @property
    def block_size(self) -> int:
        return self.page_size * self.pages_per_block


@dataclass(frozen=True)
class Partition:
    name: str
    raw_name: str
    start_block: int
    block_count: int
    attributes: int

    @property
    def end_block(self) -> int:
        return self.start_block + self.block_count


@dataclass
class PartitionDifference:
    partition: Partition
    different_bytes: int
    exact_pages: int
    exact_blocks: int
    left_ff_blocks: int
    right_ff_blocks: int
    changed_blocks: list[int]


@dataclass(frozen=True)
class UBIHeader:
    vid_offset: int
    data_offset: int
    image_sequence: int
    volume_id: int | None = None
    logical_number: int | None = None
    sequence_number: int | None = None

    @property
    def has_volume_identifier(self) -> bool:
        return self.volume_id is not None


def positive_integer(value: str) -> int:
    number = int(value, 0)
    if number <= 0:
        raise argparse.ArgumentTypeError("the value must be greater than zero")
    return number


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Compare two QDC507 full-NAND images by MIBIB partition, NAND page, "
            "erase block, and UBI logical erase block."
        )
    )
    parser.add_argument("left", type=Path, help="first full-NAND image")
    parser.add_argument("right", type=Path, help="second full-NAND image")
    parser.add_argument(
        "--page-size",
        type=positive_integer,
        default=2048,
        help="NAND page size in bytes (default: 2048)",
    )
    parser.add_argument(
        "--pages-per-block",
        type=positive_integer,
        default=64,
        help="NAND pages in one erase block (default: 64)",
    )
    parser.add_argument(
        "--no-details",
        action="store_true",
        help="omit changed-block runs and page or UBI detail",
    )
    return parser.parse_args()


def read_image(path: Path) -> bytes:
    try:
        return path.read_bytes()
    except OSError as error:
        raise AnalysisError(f"cannot read {path}: {error}") from error


def decode_partition_name(raw: bytes) -> tuple[str, str]:
    encoded = raw.split(b"\x00", 1)[0]
    if not encoded:
        raise AnalysisError("the MIBIB partition table contains an empty name")
    try:
        raw_name = encoded.decode("ascii")
    except UnicodeDecodeError as error:
        raise AnalysisError("the MIBIB partition table contains a non-ASCII name") from error
    if any(character < " " or character > "~" for character in raw_name):
        raise AnalysisError("the MIBIB partition table contains an invalid name")
    name = raw_name.split(":", 1)[-1]
    return name, raw_name


def parse_partition_table(image: bytes, geometry: Geometry) -> tuple[int, list[Partition]]:
    search_offset = 0
    image_blocks = len(image) // geometry.block_size
    candidates: list[tuple[int, list[Partition]]] = []

    while True:
        table_offset = image.find(MIBIB_MAGIC, search_offset)
        if table_offset < 0:
            break
        search_offset = table_offset + 1
        if table_offset + MIBIB_HEADER.size > len(image):
            continue

        _, _, version, entry_count = MIBIB_HEADER.unpack_from(image, table_offset)
        if version == 0 or version > 16 or entry_count == 0 or entry_count > 128:
            continue
        table_end = table_offset + MIBIB_HEADER.size + entry_count * MIBIB_ENTRY.size
        if table_end > len(image):
            continue

        partitions: list[Partition] = []
        valid = True
        for index in range(entry_count):
            entry_offset = table_offset + MIBIB_HEADER.size + index * MIBIB_ENTRY.size
            raw_name, start_block, block_count, attributes = MIBIB_ENTRY.unpack_from(
                image, entry_offset
            )
            try:
                name, full_name = decode_partition_name(raw_name)
            except AnalysisError:
                valid = False
                break
            if block_count == 0 or start_block + block_count > image_blocks:
                valid = False
                break
            partitions.append(
                Partition(name, full_name, start_block, block_count, attributes)
            )

        if valid and partitions:
            candidates.append((table_offset, partitions))

    if not candidates:
        raise AnalysisError("a valid MIBIB partition table was not found")

    candidates.sort(key=lambda candidate: candidate[0])
    return candidates[0]


def compare_partition(
    left: bytes,
    right: bytes,
    partition: Partition,
    geometry: Geometry,
) -> PartitionDifference:
    block_size = geometry.block_size
    page_size = geometry.page_size
    ff_block = b"\xff" * block_size
    different_bytes = 0
    exact_pages = 0
    exact_blocks = 0
    left_ff_blocks = 0
    right_ff_blocks = 0
    changed_blocks: list[int] = []

    for block_number in range(partition.start_block, partition.end_block):
        block_start = block_number * block_size
        block_end = block_start + block_size
        left_block = left[block_start:block_end]
        right_block = right[block_start:block_end]
        left_ff_blocks += left_block == ff_block
        right_ff_blocks += right_block == ff_block

        if left_block == right_block:
            exact_blocks += 1
            exact_pages += geometry.pages_per_block
            continue

        changed_blocks.append(block_number)
        different_bytes += sum(
            left_byte != right_byte
            for left_byte, right_byte in zip(left_block, right_block)
        )
        for page_offset in range(0, block_size, page_size):
            if (
                left_block[page_offset : page_offset + page_size]
                == right_block[page_offset : page_offset + page_size]
            ):
                exact_pages += 1

    return PartitionDifference(
        partition=partition,
        different_bytes=different_bytes,
        exact_pages=exact_pages,
        exact_blocks=exact_blocks,
        left_ff_blocks=left_ff_blocks,
        right_ff_blocks=right_ff_blocks,
        changed_blocks=changed_blocks,
    )


def contiguous_runs(values: Iterable[int]) -> list[tuple[int, int]]:
    runs: list[list[int]] = []
    for value in values:
        if not runs or value > runs[-1][1] + 1:
            runs.append([value, value])
        else:
            runs[-1][1] = value
    return [(start, end) for start, end in runs]


def format_runs(values: Iterable[int], base: int) -> str:
    formatted: list[str] = []
    for start, end in contiguous_runs(values):
        relative_start = start - base
        relative_end = end - base
        if start == end:
            formatted.append(f"{start} (partition +{relative_start})")
        else:
            formatted.append(
                f"{start}-{end} (partition +{relative_start}-+{relative_end})"
            )
    return ", ".join(formatted) if formatted else "none"


def parse_ubi_header(block: bytes) -> UBIHeader | None:
    if block[:4] != b"UBI#" or len(block) < 64:
        return None
    vid_offset = struct.unpack_from(">I", block, 16)[0]
    data_offset = struct.unpack_from(">I", block, 20)[0]
    image_sequence = struct.unpack_from(">I", block, 24)[0]
    if (
        vid_offset + 64 > len(block)
        or data_offset > len(block)
        or block[vid_offset : vid_offset + 4] != b"UBI!"
    ):
        return UBIHeader(vid_offset, data_offset, image_sequence)
    return UBIHeader(
        vid_offset=vid_offset,
        data_offset=data_offset,
        image_sequence=image_sequence,
        volume_id=struct.unpack_from(">I", block, vid_offset + 8)[0],
        logical_number=struct.unpack_from(">I", block, vid_offset + 12)[0],
        sequence_number=struct.unpack_from(">Q", block, vid_offset + 40)[0],
    )


def partition_blocks(
    image: bytes, partition: Partition, geometry: Geometry
) -> Iterable[tuple[int, bytes]]:
    for block_number in range(partition.start_block, partition.end_block):
        start = block_number * geometry.block_size
        yield block_number, image[start : start + geometry.block_size]


def ubi_physical_summary(
    image: bytes, partition: Partition, geometry: Geometry
) -> tuple[int, int, set[tuple[int, int, int]]]:
    erase_headers = 0
    volume_headers = 0
    layouts: set[tuple[int, int, int]] = set()
    for _, block in partition_blocks(image, partition, geometry):
        header = parse_ubi_header(block)
        if header is None:
            continue
        erase_headers += 1
        layouts.add((header.vid_offset, header.data_offset, header.image_sequence))
        volume_headers += header.has_volume_identifier
    return erase_headers, volume_headers, layouts


def ubi_logical_map(
    image: bytes, partition: Partition, geometry: Geometry
) -> dict[tuple[int, int], tuple[int, int, bytes]]:
    logical: dict[tuple[int, int], tuple[int, int, bytes]] = {}
    for block_number, block in partition_blocks(image, partition, geometry):
        header = parse_ubi_header(block)
        if (
            header is None
            or not header.has_volume_identifier
            or header.volume_id is None
            or header.logical_number is None
            or header.sequence_number is None
            or header.volume_id >= UBI_LAYOUT_VOLUME_ID
        ):
            continue
        key = (header.volume_id, header.logical_number)
        payload_digest = hashlib.sha256(block[header.data_offset :]).digest()
        current = logical.get(key)
        if current is None or header.sequence_number > current[0]:
            logical[key] = (header.sequence_number, block_number, payload_digest)
    return logical


def count_ubi_area_differences(
    left: bytes,
    right: bytes,
    partition: Partition,
    geometry: Geometry,
) -> Counter[str]:
    differences: Counter[str] = Counter()
    page_size = geometry.page_size
    block_size = geometry.block_size
    for block_number in range(partition.start_block, partition.end_block):
        start = block_number * block_size
        left_block = left[start : start + block_size]
        right_block = right[start : start + block_size]
        areas = (
            ("erase-header-page", 0, page_size),
            ("volume-header-page", page_size, 2 * page_size),
            ("payload", 2 * page_size, block_size),
        )
        for name, area_start, area_end in areas:
            differences[name] += sum(
                left_byte != right_byte
                for left_byte, right_byte in zip(
                    left_block[area_start:area_end], right_block[area_start:area_end]
                )
            )
    return differences


def print_ubi_detail(
    left: bytes,
    right: bytes,
    difference: PartitionDifference,
    geometry: Geometry,
) -> bool:
    partition = difference.partition
    left_headers, left_vids, left_layouts = ubi_physical_summary(
        left, partition, geometry
    )
    right_headers, right_vids, right_layouts = ubi_physical_summary(
        right, partition, geometry
    )
    if left_headers == 0 and right_headers == 0:
        return False

    print("  UBI physical layout:")
    print(
        f"    left: erase headers={left_headers}, volume headers={left_vids}; "
        f"right: erase headers={right_headers}, volume headers={right_vids}"
    )
    left_geometry = {(vid, data) for vid, data, _ in left_layouts}
    right_geometry = {(vid, data) for vid, data, _ in right_layouts}
    print(
        f"    VID/data offsets: left={sorted(left_geometry)}, "
        f"right={sorted(right_geometry)}"
    )
    area_differences = count_ubi_area_differences(
        left, right, partition, geometry
    )
    print(
        "    differing bytes: "
        + ", ".join(
            f"{name}={area_differences[name]}"
            for name in ("erase-header-page", "volume-header-page", "payload")
        )
    )

    left_logical = ubi_logical_map(left, partition, geometry)
    right_logical = ubi_logical_map(right, partition, geometry)
    common = set(left_logical) & set(right_logical)
    same_payload = sum(
        left_logical[key][2] == right_logical[key][2] for key in common
    )
    moved = sum(left_logical[key][1] != right_logical[key][1] for key in common)
    print(
        "    current logical erase blocks: "
        f"left={len(left_logical)}, right={len(right_logical)}, common={len(common)}, "
        f"same payload={same_payload}, changed payload={len(common) - same_payload}, "
        f"moved physical block={moved}, left-only={len(set(left_logical) - common)}, "
        f"right-only={len(set(right_logical) - common)}"
    )
    return True


def print_page_detail(
    left: bytes,
    right: bytes,
    difference: PartitionDifference,
    geometry: Geometry,
) -> None:
    partition = difference.partition
    page_size = geometry.page_size
    block_size = geometry.block_size
    ff_page = b"\xff" * page_size
    zero_page = b"\x00" * page_size
    classes: Counter[str] = Counter()
    page_locations: list[defaultdict[bytes, list[int]]] = [
        defaultdict(list),
        defaultdict(list),
    ]

    for side, image in enumerate((left, right)):
        for block_number in range(partition.start_block, partition.end_block):
            block_start = block_number * block_size
            block = image[block_start : block_start + block_size]
            for page_in_block in range(geometry.pages_per_block):
                page_start = page_in_block * page_size
                page = block[page_start : page_start + page_size]
                if page not in (ff_page, zero_page):
                    relative_page = (
                        (block_number - partition.start_block)
                        * geometry.pages_per_block
                        + page_in_block
                    )
                    page_locations[side][hashlib.sha256(page).digest()].append(
                        relative_page
                    )

    partition_start = partition.start_block * block_size
    partition_end = partition.end_block * block_size
    left_partition = left[partition_start:partition_end]
    right_partition = right[partition_start:partition_end]
    for page_start in range(0, len(left_partition), page_size):
        left_page = left_partition[page_start : page_start + page_size]
        right_page = right_partition[page_start : page_start + page_size]
        if left_page == right_page:
            category = "equal"
        elif left_page == ff_page:
            category = "left-ff-only"
        elif right_page == ff_page:
            category = "right-ff-only"
        elif left_page == zero_page:
            category = "left-zero-only"
        elif right_page == zero_page:
            category = "right-zero-only"
        else:
            category = "both-nonblank-different"
        classes[category] += 1

    common = set(page_locations[0]) & set(page_locations[1])
    unique_pairs = [
        (page_locations[0][digest][0], page_locations[1][digest][0])
        for digest in common
        if len(page_locations[0][digest]) == 1
        and len(page_locations[1][digest]) == 1
    ]
    same_position = sum(left_page == right_page for left_page, right_page in unique_pairs)
    print(
        "  Page classes: "
        + ", ".join(f"{name}={count}" for name, count in sorted(classes.items()))
    )
    print(
        "  Nonblank page mapping: "
        f"common digests={len(common)}, unique pairs={len(unique_pairs)}, "
        f"same position={same_position}, moved={len(unique_pairs) - same_position}"
    )


def validate_images(left: bytes, right: bytes, geometry: Geometry) -> None:
    if len(left) != len(right):
        raise AnalysisError(
            f"image sizes differ: left={len(left)} bytes, right={len(right)} bytes"
        )
    if len(left) == 0:
        raise AnalysisError("the images are empty")
    if len(left) % geometry.block_size != 0:
        raise AnalysisError(
            f"image size {len(left)} is not a multiple of block size {geometry.block_size}"
        )


def partition_layout(partitions: list[Partition]) -> list[tuple[str, int, int, int]]:
    return [
        (partition.raw_name, partition.start_block, partition.block_count, partition.attributes)
        for partition in partitions
    ]


def run() -> int:
    arguments = parse_arguments()
    geometry = Geometry(arguments.page_size, arguments.pages_per_block)
    left = read_image(arguments.left)
    right = read_image(arguments.right)
    validate_images(left, right, geometry)

    left_table_offset, left_partitions = parse_partition_table(left, geometry)
    right_table_offset, right_partitions = parse_partition_table(right, geometry)
    if partition_layout(left_partitions) != partition_layout(right_partitions):
        raise AnalysisError("the MIBIB partition layouts differ")

    differences = [
        compare_partition(left, right, partition, geometry)
        for partition in left_partitions
    ]
    total_different = sum(item.different_bytes for item in differences)
    total_pages = len(left) // geometry.page_size
    total_blocks = len(left) // geometry.block_size
    exact_pages = sum(item.exact_pages for item in differences)
    exact_blocks = sum(item.exact_blocks for item in differences)

    print("NAND image comparison")
    print(f"  left:  {arguments.left}")
    print(f"  right: {arguments.right}")
    print(
        f"  geometry: page={geometry.page_size} bytes, "
        f"pages/block={geometry.pages_per_block}, block={geometry.block_size} bytes"
    )
    print(
        f"  size: {len(left)} bytes ({len(left) / 1048576:.3f} MiB), "
        f"MIBIB table offsets: left=0x{left_table_offset:X}, "
        f"right=0x{right_table_offset:X}"
    )
    print(
        f"  different bytes: {total_different} ({total_different / len(left):.4%}); "
        f"exact pages: {exact_pages}/{total_pages}; "
        f"exact blocks: {exact_blocks}/{total_blocks}"
    )
    print()
    print(
        "Partition       Start  Blocks   Size MiB   Diff bytes    Diff %  "
        "Exact pages  Exact blocks  Diff share"
    )
    for item in differences:
        partition = item.partition
        size = partition.block_count * geometry.block_size
        page_count = partition.block_count * geometry.pages_per_block
        share = (
            item.different_bytes / total_different if total_different else 0.0
        )
        print(
            f"{partition.name:<15} {partition.start_block:>5} "
            f"{partition.block_count:>7} {size / 1048576:>10.3f} "
            f"{item.different_bytes:>12} {item.different_bytes / size:>9.3%} "
            f"{item.exact_pages:>6}/{page_count:<6} "
            f"{item.exact_blocks:>6}/{partition.block_count:<6} {share:>10.3%}"
        )

    if arguments.no_details:
        return 0

    for item in differences:
        if item.different_bytes == 0:
            continue
        partition = item.partition
        print()
        print(f"[{partition.name}]")
        print(
            "  Changed physical erase blocks: "
            + format_runs(item.changed_blocks, partition.start_block)
        )
        print(
            f"  All-FF physical blocks: left={item.left_ff_blocks}, "
            f"right={item.right_ff_blocks}"
        )
        if not print_ubi_detail(left, right, item, geometry):
            print_page_detail(left, right, item, geometry)

    return 0


def main() -> None:
    try:
        raise SystemExit(run())
    except AnalysisError as error:
        print(f"analyze-nand-diff: {error}", file=sys.stderr)
        raise SystemExit(2) from error


if __name__ == "__main__":
    main()
