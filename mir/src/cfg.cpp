#include "cfg.h"
#include <algorithm>

namespace maml::mir {

std::vector<BasicBlock*> Graph::sortedBlocks() const
{
    if (blocks.empty()) {
        return {};
    }

    std::vector<BlockID> ids;
    ids.reserve(blocks.size());
    for (const auto& [id, block] : blocks) {
        ids.push_back(id);
    }

    std::sort(ids.begin(), ids.end());

    std::vector<BasicBlock*> sorted;
    sorted.reserve(ids.size());
    for (BlockID id : ids) {
        sorted.push_back(blocks.at(id).get());
    }

    return sorted;
}

} // namespace maml::mir