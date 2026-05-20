#include <fstream>
#include <iostream>
#include <nlohmann/json.hpp>
#include <string>
#include <unordered_map>
#include <vector>

#include <llvm/IR/DataLayout.h>
#include <llvm/IR/IRBuilder.h>
#include <llvm/IR/Intrinsics.h>
#include <llvm/IR/LLVMContext.h>
#include <llvm/IR/Module.h>
#include <llvm/IR/Verifier.h>

using json = nlohmann::json;

class CodegenBackend {
public:
  llvm::LLVMContext Context;
  std::unique_ptr<llvm::Module> Module;
  std::unique_ptr<llvm::IRBuilder<>> Builder;
  std::unordered_map<std::string, llvm::Type *> TypeCache;
  // Symbol table to map variable names to live LLVM values
  std::unordered_map<std::string, llvm::Value *> SymbolTable;

  CodegenBackend(const std::string &moduleName) {
    Module = std::make_unique<llvm::Module>(moduleName, Context);
    Builder = std::make_unique<llvm::IRBuilder<>>(Context);
  }

  llvm::Type *llvmTypeFor(const json &typeJson) {
    if (typeJson.is_null())
      return llvm::Type::getVoidTy(Context);

    std::string kind = typeJson["kind"];

    // Fast path: check cache (using JSON dump as a unique signature)
    std::string cacheKey = typeJson.dump();
    if (TypeCache.find(cacheKey) != TypeCache.end()) {
      return TypeCache[cacheKey];
    }

    llvm::Type *resultType = nullptr;

    // --- Primitives ---
    if (kind == "Int") {
      resultType = llvm::Type::getInt32Ty(Context);
    } else if (kind == "Bool") {
      resultType = llvm::Type::getInt1Ty(Context);
    } else if (kind == "Unit") {
      resultType = llvm::Type::getVoidTy(Context);
    } else if (kind == "String") {
      // Layout: { i8* chars, i32 len }
      resultType =
          llvm::StructType::get(Context, {llvm::PointerType::getUnqual(Context),
                                          llvm::Type::getInt32Ty(Context)});
    } else if (kind == "Task") {
      // Coroutine handles are passed as opaque pointers
      resultType = llvm::PointerType::getUnqual(Context);
    }

    // --- Containers ---
    else if (kind == "Struct") {
      std::string structName = typeJson["name"];

      // Check if the structure type is already defined in the LLVM context
      if (llvm::StructType *existing = llvm::StructType::getTypeByName(
              Module->getContext(), structName)) {
        return existing;
      }

      // Create an identified opaque struct type
      llvm::StructType *structType =
          llvm::StructType::create(Context, structName);

      // Cache it immediately to allow recursive definitions
      TypeCache[cacheKey] = structType;

      // Define the fields
      // Note: You may need to pass the field list in the JSON structure
      // or lookup the struct definition in a global registry.
      if (typeJson.contains("fields") && typeJson["fields"].is_array()) {
        std::vector<llvm::Type *> fieldTypes;
        for (const auto &field : typeJson["fields"]) {
          fieldTypes.push_back(llvmTypeFor(field["type"]));
        }
        structType->setBody(fieldTypes);
      }
      return structType;
    } else if (kind == "Array") {
      // Layout: [Size x BaseType]
      llvm::Type *baseTy = llvmTypeFor(typeJson["base"]);
      uint64_t size = typeJson["size"];
      resultType = llvm::ArrayType::get(baseTy, size);
    } else if (kind == "Slice" || kind == "Vector") {
      // Layout: { i8* raw_heap_ptr, T* typed_data_ptr, i32 len, i32 cap }
      llvm::Type *baseTy = llvmTypeFor(typeJson["base"]);
      resultType = llvm::StructType::get(
          Context, {
                       llvm::PointerType::getUnqual(Context), // raw_heap_ptr
                       llvm::PointerType::getUnqual(
                           Context), // typed_data_ptr (Opaque in LLVM 15+)
                       llvm::Type::getInt32Ty(Context), // len
                       llvm::Type::getInt32Ty(Context)  // cap
                   });
    } else if (kind == "Map") {
      // Layout: i8* (Opaque pointer to the Zig runtime map header)
      resultType = llvm::PointerType::getUnqual(Context);
    }

    // --- User Defined Types ---
    else if (kind == "SumType") {
      // Layout: { i32 discriminant, [MaxPayloadSize x i8] payload }
      llvm::DataLayout DL(Module.get());
      uint64_t maxPayloadSize = 0;

      // 1. Calculate the size of the largest variant payload
      for (const auto &variant : typeJson["variants"]) {
        std::vector<llvm::Type *> fieldTypes;
        for (const auto &field : variant["fields"]) {
          fieldTypes.push_back(llvmTypeFor(field["type"]));
        }

        if (!fieldTypes.empty()) {
          llvm::StructType *payloadStruct =
              llvm::StructType::get(Context, fieldTypes);
          uint64_t variantSize = DL.getTypeAllocSize(payloadStruct);
          if (variantSize > maxPayloadSize) {
            maxPayloadSize = variantSize;
          }
        }
      }

      // 2. Construct the Tagged Union Struct
      if (maxPayloadSize == 0) {
        // Unit-only enum (e.g., Option<None>)
        resultType =
            llvm::StructType::get(Context, llvm::Type::getInt32Ty(Context));
      } else {
        resultType = llvm::StructType::get(
            Context, {llvm::Type::getInt32Ty(Context),
                      llvm::ArrayType::get(llvm::Type::getInt8Ty(Context),
                                           maxPayloadSize)});
      }
    } else {
      // Fallback for unidentified types
      resultType = llvm::Type::getInt32Ty(Context);
    }

    TypeCache[cacheKey] = resultType;
    return resultType;
  }

  // =========================================================================
  // EXPRESSION GENERATOR
  // =========================================================================
  llvm::Value *evaluateExpression(const json &expr) {
    if (expr.is_null())
      return nullptr;

    std::string nodeType = expr.value("node_type", "unknown");

    if (nodeType == "IntLiteral") {
      int value = expr["value"].get<int>();
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), value);
    }

    if (nodeType == "BoolLiteral") {
      bool value = expr["value"];
      return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context),
                                    value ? 1 : 0);
    }

    if (nodeType == "ArrayLiteral") {
      return compileArrayLiteral(expr);
    }

    if (nodeType == "Identifier") {
      std::string varName = expr["value"];
      llvm::Value *val = resolveSymbol(varName);

      if (!val) {
        std::cerr << "CRITICAL ERROR: Variable '" << varName
                  << "' not found!\n";
        return nullptr;
      }

      // --- FIXED: Only load if it's NOT an array type ---
      if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(val)) {
        llvm::Type *allocatedType = alloca->getAllocatedType();

        // If it's an array, we return the pointer (alloca) directly.
        // If it's an int/struct, we load the value.
        if (allocatedType->isArrayTy()) {
          return alloca;
        }

        return Builder->CreateLoad(allocatedType, alloca, varName);
      }

      return val;
    }

    // --- Prefix Expressions ---
    if (nodeType == "PrefixExpr") {
      llvm::Value *right = evaluateExpression(expr["right"]);
      std::string op = expr["operator"];

      if (op == "!") {
        // Logical NOT is XOR with true (1)
        return Builder->CreateXor(
            right, llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 1),
            "nottmp");
      }
      if (op == "-") {
        // Arithmetic negation is subtracting from 0
        return Builder->CreateSub(
            llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0), right,
            "negtmp");
      }
    }

    // --- Infix Expressions ---
    if (nodeType == "InfixExpr") {
      std::string op = expr["operator"];

      // Evaluate the left side first for all infix operations
      llvm::Value *left = evaluateExpression(expr["left"]);

      // 1. Short-circuiting operators (Do NOT evaluate right side yet)
      if (op == "&&")
        return compileLogicalAnd(expr, left);
      if (op == "||")
        return compileLogicalOr(expr, left);

      // 2. Non-short-circuiting operators (Safe to evaluate right side now)
      llvm::Value *right = evaluateExpression(expr["right"]);

      // Arithmetic
      if (op == "+")
        return Builder->CreateAdd(left, right, "addtmp");
      if (op == "-")
        return Builder->CreateSub(left, right, "subtmp");
      if (op == "*")
        return Builder->CreateMul(left, right, "multmp");
      if (op == "/")
        return Builder->CreateSDiv(left, right, "divtmp"); // Signed Division
      if (op == "%")
        return Builder->CreateSRem(left, right, "remtmp"); // Signed Remainder

      // Relational
      if (op == "==")
        return Builder->CreateICmpEQ(left, right, "eqtmp");
      if (op == "!=")
        return Builder->CreateICmpNE(left, right, "netmp");
      if (op == "<")
        return Builder->CreateICmpSLT(left, right, "lttmp"); // Signed Less Than
      if (op == "<=")
        return Builder->CreateICmpSLE(left, right, "letmp");
      if (op == ">")
        return Builder->CreateICmpSGT(left, right, "gttmp");
      if (op == ">=")
        return Builder->CreateICmpSGE(left, right, "getmp");
    }

    if (nodeType == "IfExpr") {
      return compileIfExpr(expr);
    }

    if (nodeType == "BlockStmt") {
      pushScope();
      llvm::Value *blockResult = nullptr;
      for (const auto &s : expr["statements"]) {
        if (s["node_type"] == "YieldStmt") {
          blockResult = evaluateExpression(s["value"]);
        } else {
          compileStatement(s);
        }
        // Stop processing immediately if a Return terminated the block early
        if (Builder->GetInsertBlock()->getTerminator()) {
          break;
        }
      }
      popScope();
      return blockResult;
    }

    if (nodeType == "FieldAccess") {
      llvm::Value *fieldPtr = compileFieldAccess(expr);
      if (!fieldPtr)
        return nullptr;

      llvm::Type *fieldTy = llvmTypeFor(expr["maml_type"]);

      // If it's an array, return the pointer directly.
      // For primitives (Int, Bool) and Structs, load the value from memory.
      if (fieldTy->isArrayTy()) {
        return fieldPtr;
      }
      return Builder->CreateLoad(fieldTy, fieldPtr, "field_load");
    }

    // =========================================================================
    // PHASE 2: COMPLEX DATA TYPES & CONSTANTS
    // =========================================================================

    // --- String Literals ---
    if (nodeType == "StringLiteral") {
      std::string strVal = expr["value"];
      llvm::Type *strTy = llvmTypeFor(expr["maml_type"]);

      // 1. Emit the actual char data into the global data section (.rodata)
      // true = null-terminate the string.
      llvm::Constant *strConst =
          llvm::ConstantDataArray::getString(Context, strVal, true);
      llvm::GlobalVariable *globalStr = new llvm::GlobalVariable(
          *Module, strConst->getType(), true, llvm::GlobalValue::PrivateLinkage,
          strConst, "str_lit");

      // 2. Allocate the String Header { i8* ptr, i32 len } on the stack
      llvm::AllocaInst *alloca =
          Builder->CreateAlloca(strTy, nullptr, "str_header");

      // 3. Populate field 0: Pointer to the global chars
      llvm::Value *charPtr = Builder->CreatePointerCast(
          globalStr, llvm::PointerType::getUnqual(Context));
      llvm::Value *ptrGep = Builder->CreateGEP(
          strTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
      Builder->CreateStore(charPtr, ptrGep);

      // 4. Populate field 1: Length of the string
      llvm::Value *lenGep = Builder->CreateGEP(
          strTy, alloca, {Builder->getInt32(0), Builder->getInt32(1)});
      Builder->CreateStore(Builder->getInt32(strVal.length()), lenGep);

      return alloca;
    }

    // --- Struct Literals ---
    if (nodeType == "StructLiteral") {
      llvm::Type *structTy = llvmTypeFor(expr["maml_type"]);
      llvm::AllocaInst *alloca =
          Builder->CreateAlloca(structTy, nullptr, "struct_lit");

      json typeJson = expr["maml_type"];
      for (const auto &field : expr["fields"]) {
        std::string fieldName = field["name"];
        llvm::Value *fieldVal = evaluateExpression(field["value"]);

        // Resolve structural index from type definition
        int index = -1;
        for (int i = 0; i < typeJson["fields"].size(); ++i) {
          if (typeJson["fields"][i]["name"] == fieldName) {
            index = i;
            break;
          }
        }

        // Store value into struct layout
        llvm::Value *gep = Builder->CreateGEP(
            structTy, alloca, {Builder->getInt32(0), Builder->getInt32(index)});
        Builder->CreateStore(fieldVal, gep);
      }
      return alloca;
    }

    // --- Tagged Union / Variant Literals ---
    if (nodeType == "VariantLiteral") {
      llvm::Type *sumTy = llvmTypeFor(expr["maml_type"]);
      llvm::AllocaInst *alloca =
          Builder->CreateAlloca(sumTy, nullptr, "variant_lit");

      std::string variantName = expr["variant_name"];
      json typeJson = expr["maml_type"];

      // Retrieve discriminant ID and field layout for this specific variant
      int discriminant = 0;
      json variantDef;
      for (const auto &v : typeJson["variants"]) {
        if (v["name"] == variantName) {
          discriminant = v["discriminant"];
          variantDef = v;
          break;
        }
      }

      // 1. Store discriminant into index 0
      llvm::Value *discrimGep = Builder->CreateGEP(
          sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(0)});
      Builder->CreateStore(Builder->getInt32(discriminant), discrimGep);

      // 2. Build and populate the payload struct into index 1 (if it's not a
      // unit variant)
      if (!expr["fields"].empty()) {
        llvm::Value *payloadGep = Builder->CreateGEP(
            sumTy, alloca, {Builder->getInt32(0), Builder->getInt32(1)});

        // Construct a transient struct type so LLVM calculates
        // alignment/padding correctly
        std::vector<llvm::Type *> payloadTypes;
        for (const auto &f : variantDef["fields"]) {
          payloadTypes.push_back(llvmTypeFor(f["type"]));
        }
        llvm::StructType *payloadStructTy =
            llvm::StructType::get(Context, payloadTypes);

        // Bitcast the [MaxPayloadSize x i8] byte array slot into the typed
        // payload pointer
        llvm::Value *castedPayloadPtr = Builder->CreatePointerCast(
            payloadGep, llvm::PointerType::getUnqual(Context));

        for (const auto &field : expr["fields"]) {
          std::string fieldName = field["name"];
          llvm::Value *fieldVal = evaluateExpression(field["value"]);

          int index = -1;
          for (int i = 0; i < variantDef["fields"].size(); ++i) {
            if (variantDef["fields"][i]["name"] == fieldName) {
              index = i;
              break;
            }
          }

          llvm::Value *fieldGep = Builder->CreateGEP(
              payloadStructTy, castedPayloadPtr,
              {Builder->getInt32(0), Builder->getInt32(index)});
          Builder->CreateStore(fieldVal, fieldGep);
        }
      }
      return alloca;
    }

    // --- Slicing Operations (Arrays & Slices) ---
    if (nodeType == "SliceExpr") {
      llvm::Value *leftVal = evaluateExpression(expr["left"]); // Base pointer
      llvm::Type *leftTy = llvmTypeFor(expr["left"]["maml_type"]);

      // Evaluate bounds
      llvm::Value *lowVal = expr.contains("low") && !expr["low"].is_null()
                                ? evaluateExpression(expr["low"])
                                : Builder->getInt32(0);

      llvm::Value *highVal = nullptr;
      llvm::Value *originalCap = nullptr;
      llvm::Value *originalRawPtr = nullptr;
      llvm::Value *originalDataPtr = nullptr;

      std::string leftKind = expr["left"]["maml_type"]["kind"];

      if (leftKind == "Array") {
        // Source is a fixed-size array: [N x T]
        int size = expr["left"]["maml_type"]["size"];
        originalCap = Builder->getInt32(size);

        // Base heap pointer is the array pointer itself
        originalRawPtr = Builder->CreatePointerCast(
            leftVal, llvm::PointerType::getUnqual(Context));

        // Data pointer (T*) is a GEP to index 0 of the array
        originalDataPtr = Builder->CreateGEP(
            leftTy, leftVal, {Builder->getInt32(0), Builder->getInt32(0)});

        highVal = expr.contains("high") && !expr["high"].is_null()
                      ? evaluateExpression(expr["high"])
                      : originalCap;

      } else if (leftKind == "Slice" || leftKind == "Vector") {
        // Source is already a container: { i8* raw, T* data, i32 len, i32 cap }
        llvm::Value *rawGep = Builder->CreateGEP(
            leftTy, leftVal, {Builder->getInt32(0), Builder->getInt32(0)});
        llvm::Value *dataGep = Builder->CreateGEP(
            leftTy, leftVal, {Builder->getInt32(0), Builder->getInt32(1)});
        llvm::Value *lenGep = Builder->CreateGEP(
            leftTy, leftVal, {Builder->getInt32(0), Builder->getInt32(2)});
        llvm::Value *capGep = Builder->CreateGEP(
            leftTy, leftVal, {Builder->getInt32(0), Builder->getInt32(3)});

        originalRawPtr =
            Builder->CreateLoad(llvm::PointerType::getUnqual(Context), rawGep);
        originalDataPtr =
            Builder->CreateLoad(llvm::PointerType::getUnqual(Context), dataGep);
        originalCap =
            Builder->CreateLoad(llvm::Type::getInt32Ty(Context), capGep);

        highVal =
            expr.contains("high") && !expr["high"].is_null()
                ? evaluateExpression(expr["high"])
                : Builder->CreateLoad(llvm::Type::getInt32Ty(Context), lenGep);
      }

      // Compute new header state
      llvm::Value *newLen = Builder->CreateSub(highVal, lowVal, "slice_len");
      llvm::Value *newCap =
          Builder->CreateSub(originalCap, lowVal, "slice_cap");

      // Compute new offset data pointer: originalDataPtr + lowVal
      llvm::Type *baseTy = llvmTypeFor(expr["maml_type"]["base"]);
      llvm::Value *newDataPtr =
          Builder->CreateGEP(baseTy, originalDataPtr, lowVal, "slice_data_ptr");

      // Build the new Slice struct
      llvm::Type *sliceTy = llvmTypeFor(expr["maml_type"]);
      llvm::AllocaInst *sliceAlloca =
          Builder->CreateAlloca(sliceTy, nullptr, "slice_header");

      Builder->CreateStore(
          originalRawPtr,
          Builder->CreateGEP(sliceTy, sliceAlloca,
                             {Builder->getInt32(0), Builder->getInt32(0)}));
      Builder->CreateStore(
          newDataPtr,
          Builder->CreateGEP(sliceTy, sliceAlloca,
                             {Builder->getInt32(0), Builder->getInt32(1)}));
      Builder->CreateStore(newLen, Builder->CreateGEP(sliceTy, sliceAlloca,
                                                      {Builder->getInt32(0),
                                                       Builder->getInt32(2)}));
      Builder->CreateStore(newCap, Builder->CreateGEP(sliceTy, sliceAlloca,
                                                      {Builder->getInt32(0),
                                                       Builder->getInt32(3)}));

      // --- FIX: Retain heap memory, do NOT track stack pointer directly ---
      bool isHeap = expr["left"].value("is_heap", false);
      if (isHeap || leftKind == "Slice" || leftKind == "Vector" ||
          leftKind == "String") {
        llvm::FunctionCallee retainFn = Module->getOrInsertFunction(
            "maml_retain", llvm::FunctionType::get(
                               llvm::Type::getVoidTy(Context),
                               {llvm::PointerType::getUnqual(Context)}, false));
        Builder->CreateCall(retainFn, {originalRawPtr});
      }

      return sliceAlloca;
    }

    if (nodeType == "AwaitExpr") {
      return compileAwaitExpression(expr);
    }

    // =========================================================================
    // PHASE 3: RUNTIME INTEGRATION & BUILT-INS
    // =========================================================================
    if (nodeType == "CallExpr") {
      return compileCallExpr(expr);
    }

    if (nodeType == "IndexExpr") {
      llvm::Value *leftVal = evaluateExpression(expr["left"]);
      llvm::Value *indexVal = evaluateExpression(expr["index"]);

      llvm::Type *leftTy = llvmTypeFor(expr["left"]["maml_type"]);

      // --- FIX: Spill to stack if we were handed a loaded value instead of a
      // pointer ---
      llvm::Value *leftPtr = leftVal;
      if (!leftVal->getType()->isPointerTy()) {
        llvm::AllocaInst *spill =
            Builder->CreateAlloca(leftTy, nullptr, "slice_spill");
        Builder->CreateStore(leftVal, spill);
        leftPtr = spill;
      }

      llvm::Value *elemPtr;
      if (leftTy->isArrayTy()) {
        elemPtr = Builder->CreateGEP(
            leftTy, leftPtr, {Builder->getInt32(0), indexVal}, "array_idx");
      } else {
        // Slice/Vector case: {raw, data, len, cap}
        // The data pointer is at index 1
        llvm::Value *dataPtr = Builder->CreateGEP(
            leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)},
            "data_ptr");

        llvm::Value *data =
            Builder->CreateLoad(llvm::PointerType::getUnqual(Context), dataPtr);

        elemPtr = Builder->CreateGEP(llvmTypeFor(expr["maml_type"]), data,
                                     indexVal, "slice_idx");
      }

      return Builder->CreateLoad(llvmTypeFor(expr["maml_type"]), elemPtr,
                                 "element_load");
    }

    std::cerr << "Backend Error: Unsupported expression type: " << nodeType
              << "\n";
    return nullptr;
  }

  // =========================================================================
  // STATEMENT GENERATOR
  // =========================================================================
  void compileStatement(const json &stmt) {
    if (stmt.is_null())
      return;

    std::string nodeType = stmt["node_type"];

    // 1. Block Statements: Push/Pop scope for ARC tracking
    if (nodeType == "BlockStmt") {
      pushScope(); //
      for (const auto &s : stmt["statements"]) {
        compileStatement(s);
      }
      popScope(); //
      return;
    }

    // 2. Declaration: Alloca + ARC Tracking
    if (nodeType == "DeclareStmt") {
      std::string varName = stmt["name"];
      llvm::Value *initVal = evaluateExpression(stmt["value"]);
      json typeJson = stmt["value"]["maml_type"];
      llvm::Type *valTy = llvmTypeFor(typeJson);

      bool isHeap = stmt["value"].value("is_heap", false);
      if (isHeap && (valTy->isArrayTy() || valTy->isStructTy())) {
        // initVal is already a heap pointer. Store the pointer itself.
        llvm::AllocaInst *ptrAlloca =
            Builder->CreateAlloca(initVal->getType(), nullptr, varName);
        Builder->CreateStore(initVal, ptrAlloca);
        SymbolEnv.back()[varName] = ptrAlloca;

        // We do not call trackDeepForRelease here because the heap memory
        // is already being tracked by the ArrayLiteral.
        return;
      }

      // Normal path for stack variables
      llvm::AllocaInst *alloca = Builder->CreateAlloca(valTy, nullptr, varName);

      if (initVal->getType()->isPointerTy() &&
          (valTy->isStructTy() || valTy->isArrayTy())) {
        llvm::Value *loadedData =
            Builder->CreateLoad(valTy, initVal, "array_load");
        Builder->CreateStore(loadedData, alloca);
      } else {
        Builder->CreateStore(initVal, alloca);
      }

      SymbolEnv.back()[varName] = alloca;
      trackDeepForRelease(alloca, typeJson);
      return;
    }

    // 3. Assignment: Store + ARC Tracking
    if (nodeType == "AssignStmt") {
      const json &lvalueNode =
          stmt.contains("lvalue") ? stmt["lvalue"] : stmt["target"];
      std::string lnodeType = lvalueNode.value("node_type", "unknown");

      llvm::Value *targetPtr = nullptr;
      bool isPointerReassignment = false;

      // --- FIX: Compute the memory address (LValue) dynamically ---
      if (lnodeType == "Identifier") {
        std::string targetName = lvalueNode["value"];
        targetPtr = resolveSymbol(targetName);

        // Check if we are reassigning a heap pointer natively (from our
        // previous fix)
        if (targetPtr) {
          if (auto *alloca = llvm::dyn_cast<llvm::AllocaInst>(targetPtr)) {
            if (alloca->getAllocatedType()->isPointerTy()) {
              isPointerReassignment = true;
            }
          }
        }
      } else if (lnodeType == "FieldAccess") {
        // compileFieldAccess already perfectly computes the target GEP pointer
        targetPtr = compileFieldAccess(lvalueNode);
      } else if (lnodeType == "IndexExpr") {
        llvm::Value *leftVal = evaluateExpression(lvalueNode["left"]);
        llvm::Value *indexVal = evaluateExpression(lvalueNode["index"]);
        llvm::Type *leftTy = llvmTypeFor(lvalueNode["left"]["maml_type"]);

        // Spill loaded structs/arrays to stack to guarantee a valid pointer for
        // GEP
        llvm::Value *leftPtr = leftVal;
        if (!leftVal->getType()->isPointerTy()) {
          llvm::AllocaInst *spill =
              Builder->CreateAlloca(leftTy, nullptr, "lvalue_spill");
          Builder->CreateStore(leftVal, spill);
          leftPtr = spill;
        }

        if (leftTy->isArrayTy()) {
          targetPtr = Builder->CreateGEP(leftTy, leftPtr,
                                         {Builder->getInt32(0), indexVal},
                                         "array_idx_assign");
        } else {
          // Slice/Vector: Extract the internal data pointer and index into it
          llvm::Value *dataPtr = Builder->CreateGEP(
              leftTy, leftPtr, {Builder->getInt32(0), Builder->getInt32(1)},
              "data_ptr_assign");
          llvm::Value *data = Builder->CreateLoad(
              llvm::PointerType::getUnqual(Context), dataPtr);
          targetPtr = Builder->CreateGEP(llvmTypeFor(lvalueNode["maml_type"]),
                                         data, indexVal, "slice_idx_assign");
        }
      }

      if (!targetPtr) {
        std::cerr << "Error: Invalid or undefined LValue in assignment\n";
        return;
      }

      // --- Store the RValue into the newly computed LValue pointer ---
      const json &rhsNode =
          stmt.contains("rvalue") ? stmt["rvalue"] : stmt["value"];
      llvm::Value *rhsVal = evaluateExpression(rhsNode);
      json typeJson = rhsNode["maml_type"];

      emitRelease(targetPtr, typeJson);
      llvm::Type *valTy = llvmTypeFor(typeJson);

      if (isPointerReassignment) {
        Builder->CreateStore(rhsVal, targetPtr);
      } else if (rhsVal->getType()->isPointerTy() &&
                 (valTy->isStructTy() || valTy->isArrayTy())) {
        llvm::Value *loadedData =
            Builder->CreateLoad(valTy, rhsVal, "array_load");
        Builder->CreateStore(loadedData, targetPtr);
      } else {
        Builder->CreateStore(rhsVal, targetPtr);
      }

      emitRetain(targetPtr, typeJson);
      return;
    }

    // 4. Return Statement: Handle Async Coroutine Handles vs Sync Returns
    if (nodeType == "ReturnStmt") {
      llvm::Value *retVal = evaluateExpression(stmt["value"]);

      // If inside an async function, we must return the coroutine handle
      if (llvm::Value *coroHdl = resolveSymbol("__coro_hdl")) {
        Builder->CreateRet(coroHdl);
      } else {
        if (retVal) {
          // --- FIX: Auto-load pointers if the function expects a value ---
          llvm::Function *F = Builder->GetInsertBlock()->getParent();
          llvm::Type *expectedRetTy = F->getReturnType();

          // If we have a pointer, but the function signature expects a value
          // (like a Struct)
          if (retVal->getType()->isPointerTy() &&
              !expectedRetTy->isPointerTy()) {
            retVal = Builder->CreateLoad(expectedRetTy, retVal, "ret_load");
          }

          Builder->CreateRet(retVal);
        } else {
          Builder->CreateRetVoid();
        }
      }
      return;
    }

    // 5. Yield Statement: (Used for expression blocks like 'if' results)
    if (nodeType == "YieldStmt") {
      evaluateExpression(stmt["value"]);
      return;
    }

    // 6. Expression Statement: Evaluate and discard
    if (nodeType == "ExprStmt") {
      evaluateExpression(stmt["value"]); // [cite: 84]
      return;
    }

    // 7. For Statement: Control Flow Graph (CFG) Construction
    if (nodeType == "ForStmt") {
      compileForLoop(stmt); // [cite: 89, 90]
      return;
    }
  }

  void compileCoroutinePrologue(llvm::Function *F) {
    llvm::Value *align =
        llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
    llvm::Value *nullPtr =
        llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));

    // 1. Declare and issue @llvm.coro.id
    llvm::Function *coroIdFn =
        llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_id);
    llvm::Value *coroId = Builder->CreateCall(
        coroIdFn, {align, nullPtr, nullPtr, nullPtr}, "coro.id");
    SymbolTable["__coro_id"] = coroId;

    // --- FIX: Request a 64-bit size from LLVM to match Zig's usize ---
    llvm::Function *coroSizeFn = llvm::Intrinsic::getDeclaration(
        Module.get(), llvm::Intrinsic::coro_size,
        {llvm::Type::getInt64Ty(Context)});
    llvm::Value *coroSize = Builder->CreateCall(coroSizeFn, {}, "coro.size");

    // --- FIX: Update signature to take an i64 ---
    llvm::FunctionCallee mamlAlloc = Module->getOrInsertFunction(
        "maml_alloc",
        llvm::FunctionType::get(llvm::PointerType::getUnqual(Context),
                                {llvm::Type::getInt64Ty(Context)}, false));
    llvm::Value *allocMem =
        Builder->CreateCall(mamlAlloc, {coroSize}, "coro.alloc.mem");

    // 4. Anchor state frame boundaries and record the returning token handle
    llvm::Function *coroBeginFn = llvm::Intrinsic::getDeclaration(
        Module.get(), llvm::Intrinsic::coro_begin);
    llvm::Value *coroHdl =
        Builder->CreateCall(coroBeginFn, {coroId, allocMem}, "coro.hdl");
    SymbolTable["__coro_hdl"] = coroHdl;
  }

  void compileFunction(const json &fn) {
    std::string name = fn["name"];

    // 1. Resolve Return Type
    llvm::Type *retType = llvm::Type::getVoidTy(Context);
    if (fn.contains("maml_type") && !fn["maml_type"].is_null()) {
      retType = llvmTypeFor(fn["maml_type"]);
    } else if (fn.contains("return_type") && !fn["return_type"].is_null()) {
      retType = llvmTypeFor(fn["return_type"]);
    }

    // FIX: LLVM explicitly requires the 'main' function to return i32
    if (name == "main") {
      retType = llvm::Type::getInt32Ty(Context);
    }

    std::vector<llvm::Type *> paramTypes;
    if (fn.contains("params")) {
      for (const auto &p : fn["params"]) {
        paramTypes.push_back(llvmTypeFor(p["type"]));
      }
    }

    llvm::FunctionType *FT =
        llvm::FunctionType::get(retType, paramTypes, false);
    llvm::Function *F = llvm::Function::Create(
        FT, llvm::Function::ExternalLinkage, name, *Module);

    if (fn.contains("is_async") && fn["is_async"] == true) {
      F->addFnAttr(llvm::Attribute::PresplitCoroutine);
    }

    llvm::BasicBlock *BB = llvm::BasicBlock::Create(Context, "entry", F);
    Builder->SetInsertPoint(BB);

    SymbolTable.clear();

    pushScope();

    // Fire up coroutine orchestration structures before processing statements
    if (fn.contains("is_async") && fn["is_async"] == true) {
      compileCoroutinePrologue(F);
    }

    // Map function parameters onto the local stack frame variables
    unsigned Idx = 0;
    for (auto &Arg : F->args()) {
      std::string pName = fn["params"][Idx]["name"];
      llvm::Type *pType = paramTypes[Idx];
      llvm::AllocaInst *alloca = Builder->CreateAlloca(pType, nullptr, pName);
      Builder->CreateStore(&Arg, alloca);

      // Now safe: SymbolEnv has at least one scope map inside it
      SymbolEnv.back()[pName] = alloca;

      Idx++;
    }

    if (fn.contains("body")) {
      compileStatement(fn["body"]);
    }

    // Catch-all fallthrough terminator to guarantee module validity
    if (!Builder->GetInsertBlock()->getTerminator()) {
      if (llvm::Value *coroHdl = resolveSymbol("__coro_hdl")) {
        Builder->CreateRet(coroHdl);
      } else if (name == "main") {
        Builder->CreateRet(
            llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0));
      } else if (retType->isVoidTy()) {
        Builder->CreateRetVoid();
      } else {
        Builder->CreateRet(llvm::Constant::getNullValue(retType));
      }
    }

    popScope();
  }

  struct TrackedItem {
    llvm::Value *ptr;
    bool isRaw;
    json typeJson;
  };
  std::vector<std::vector<TrackedItem>> ScopeStack;
  std::vector<std::unordered_map<std::string, llvm::Value *>> SymbolEnv;

  void pushScope() {
    ScopeStack.push_back({}); // For ARC
    SymbolEnv.push_back({});  // For Lexical Scoping
  }

  void trackForRelease(llvm::Value *ptr, bool isHeap) {
    if (!isHeap)
      return;
    // Track as a raw heap pointer (e.g. from ArrayLiteral or heap-allocated
    // Struct)
    ScopeStack.back().push_back({ptr, true, json()});
  }

  void popScope() {
    auto &currentScope = ScopeStack.back();

    // Safety check: Don't emit release calls if the block is already terminated
    if (Builder->GetInsertBlock()->getTerminator() == nullptr) {
      llvm::FunctionCallee releaseFn = Module->getOrInsertFunction(
          "maml_release", llvm::FunctionType::get(
                              llvm::Type::getVoidTy(Context),
                              {llvm::PointerType::getUnqual(Context)}, false));

      // Iterate in reverse to release the most recently allocated items first
      for (auto it = currentScope.rbegin(); it != currentScope.rend(); ++it) {
        if (it->isRaw) {
          llvm::Value *rawPtr = Builder->CreateBitCast(
              it->ptr, llvm::PointerType::getUnqual(Context));
          Builder->CreateCall(releaseFn, {rawPtr});
        } else {
          // It's a stack container: extract the internal heap pointer safely!
          emitRelease(it->ptr, it->typeJson);
        }
      }
    }
    ScopeStack.pop_back();
    SymbolEnv.pop_back();
  }

  llvm::Value *resolveSymbol(const std::string &name) {
    for (auto it = SymbolEnv.rbegin(); it != SymbolEnv.rend(); ++it) {
      if (it->find(name) != it->end()) {
        return (*it)[name];
      }
    }
    return nullptr;
  }

  void compileProgram(const json &ast);
  void compileForLoop(const json &s);
  llvm::Value *compileIfExpr(const json &e);
  llvm::Value *compileFieldAccess(const json &e);
  llvm::Value *compileMatchExpr(const json &e);
  llvm::Value *compilePatternCheck(const json &pattern, llvm::Value *subject);
  llvm::Value *compileAwaitExpression(const json &e);
  llvm::Value *compileArrayLiteral(const json &e);
  llvm::Value *compileLogicalAnd(const json &expr, llvm::Value *leftVal);
  llvm::Value *compileLogicalOr(const json &expr, llvm::Value *leftVal);
  llvm::Value *compileCallExpr(const json &expr);
  void injectPatternBindings(const json &pattern, llvm::Value *subject,
                             const json &subjectType);
  bool isHeapManagedType(const json &typeJson);
  llvm::Value *extractHeapPointer(llvm::Value *containerPtr,
                                  const json &typeJson);
  void emitRetain(llvm::Value *valPtr, const json &typeJson);
  void emitRelease(llvm::Value *valPtr, const json &typeJson);
  void trackDeepForRelease(llvm::Value *valPtr, const json &typeJson);
  void untrackFromRelease(llvm::Value *valPtr);
  bool isRefType(const json &typeJson);
  bool needsARC(const json &typeJson);
};

llvm::Value *CodegenBackend::compileCallExpr(const json &expr) {
  // --- 1. INTERCEPT METHOD CALLS ON CONTAINERS ---
  if (expr["function"]["node_type"] == "FieldAccess") {
    llvm::Value *objVal = evaluateExpression(expr["function"]["object"]);
    std::string methodName = expr["function"]["field"];
    std::string objKind = expr["function"]["object"]["maml_type"]["kind"];
    llvm::Type *objTy = llvmTypeFor(expr["function"]["object"]["maml_type"]);

    // == VECTOR BUILT-INS ==
    if (objKind == "Vector") {
      if (methodName == "len") {
        // Field 2 is 'len' in the {raw_ptr, data_ptr, len, cap} layout
        llvm::Value *lenGep = Builder->CreateGEP(
            objTy, objVal, {Builder->getInt32(0), Builder->getInt32(2)},
            "vec_len_ptr");
        return Builder->CreateLoad(llvm::Type::getInt32Ty(Context), lenGep,
                                   "vec_len");
      }
      if (methodName == "push") {
        llvm::Function *F = Builder->GetInsertBlock()->getParent();
        llvm::BasicBlock *growBB =
            llvm::BasicBlock::Create(Context, "vec_grow", F);
        llvm::BasicBlock *insertBB =
            llvm::BasicBlock::Create(Context, "vec_insert", F);

        llvm::Value *rawPtrGep = Builder->CreateGEP(
            objTy, objVal, {Builder->getInt32(0), Builder->getInt32(0)});
        llvm::Value *dataPtrGep = Builder->CreateGEP(
            objTy, objVal, {Builder->getInt32(0), Builder->getInt32(1)});
        llvm::Value *lenGep = Builder->CreateGEP(
            objTy, objVal, {Builder->getInt32(0), Builder->getInt32(2)});
        llvm::Value *capGep = Builder->CreateGEP(
            objTy, objVal, {Builder->getInt32(0), Builder->getInt32(3)});

        llvm::Value *currentLen =
            Builder->CreateLoad(llvm::Type::getInt32Ty(Context), lenGep);
        llvm::Value *currentCap =
            Builder->CreateLoad(llvm::Type::getInt32Ty(Context), capGep);

        llvm::Value *isFull =
            Builder->CreateICmpEQ(currentLen, currentCap, "is_full");
        Builder->CreateCondBr(isFull, growBB, insertBB);

        // --- GROW BLOCK (Calls runtime.zig: maml_vec_grow) ---
        Builder->SetInsertPoint(growBB);
        llvm::Value *currentRawPtr = Builder->CreateLoad(
            llvm::PointerType::getUnqual(Context), rawPtrGep);

        llvm::Type *baseTy =
            llvmTypeFor(expr["function"]["object"]["maml_type"]["base"]);
        llvm::DataLayout DL(Module.get());
        llvm::Value *itemSize = Builder->getInt32(DL.getTypeAllocSize(baseTy));

        llvm::FunctionCallee vecGrowFn = Module->getOrInsertFunction(
            "maml_vec_grow",
            llvm::FunctionType::get(llvm::PointerType::getUnqual(Context),
                                    {llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt32Ty(Context),
                                     llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt32Ty(Context)},
                                    false));

        llvm::Value *newRawPtr = Builder->CreateCall(
            vecGrowFn, {currentRawPtr, currentLen, capGep, itemSize});
        Builder->CreateStore(newRawPtr, rawPtrGep);
        Builder->CreateStore(newRawPtr, dataPtrGep);
        Builder->CreateBr(insertBB);

        // --- INSERT BLOCK ---
        Builder->SetInsertPoint(insertBB);
        llvm::Value *freshDataPtr = Builder->CreateLoad(
            llvm::PointerType::getUnqual(Context), dataPtrGep);
        llvm::Value *freshLen =
            Builder->CreateLoad(llvm::Type::getInt32Ty(Context), lenGep);
        llvm::Value *itemVal =
            evaluateExpression(expr["arguments"][0]["value"]);

        llvm::Value *targetElemPtr = Builder->CreateGEP(
            baseTy, freshDataPtr, freshLen, "target_elem_ptr");
        Builder->CreateStore(itemVal, targetElemPtr);

        llvm::Value *newLen =
            Builder->CreateAdd(freshLen, Builder->getInt32(1));
        Builder->CreateStore(newLen, lenGep);

        return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
      }
    }

    // == MAP BUILT-INS ==
    if (objKind == "Map") {
      llvm::Value *mapPtr = Builder->CreateLoad(
          llvm::PointerType::getUnqual(Context), objVal, "map_ptr_load");
      llvm::Value *keyVal = evaluateExpression(expr["arguments"][0]["value"]);

      llvm::Value *keyHash = nullptr;
      llvm::Value *strPtr =
          llvm::ConstantPointerNull::get(llvm::PointerType::getUnqual(Context));
      llvm::Value *strLen = Builder->getInt32(0);

      if (expr["arguments"][0]["value"]["maml_type"]["kind"] == "String") {
        llvm::Type *strTy =
            llvmTypeFor(expr["arguments"][0]["value"]["maml_type"]);
        llvm::Value *strPtrGep = Builder->CreateGEP(
            strTy, keyVal, {Builder->getInt32(0), Builder->getInt32(0)});
        llvm::Value *strLenGep = Builder->CreateGEP(
            strTy, keyVal, {Builder->getInt32(0), Builder->getInt32(1)});
        strPtr = Builder->CreateLoad(llvm::PointerType::getUnqual(Context),
                                     strPtrGep);
        strLen =
            Builder->CreateLoad(llvm::Type::getInt32Ty(Context), strLenGep);

        llvm::FunctionCallee strHashFn = Module->getOrInsertFunction(
            "maml_str_hash",
            llvm::FunctionType::get(llvm::Type::getInt64Ty(Context),
                                    {llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt32Ty(Context)},
                                    false));
        keyHash = Builder->CreateCall(strHashFn, {strPtr, strLen});
      } else {
        keyHash = Builder->CreateZExt(keyVal, llvm::Type::getInt64Ty(Context));
      }

      if (methodName == "put") {
        llvm::Value *valVal = evaluateExpression(expr["arguments"][1]["value"]);
        llvm::Type *valTy =
            llvmTypeFor(expr["arguments"][1]["value"]["maml_type"]);

        llvm::AllocaInst *valAlloca =
            Builder->CreateAlloca(valTy, nullptr, "map_val_tmp");
        Builder->CreateStore(valVal, valAlloca);
        llvm::Value *valRawPtr = Builder->CreatePointerCast(
            valAlloca, llvm::PointerType::getUnqual(Context));

        llvm::FunctionCallee mapPutFn = Module->getOrInsertFunction(
            "maml_map_put",
            llvm::FunctionType::get(llvm::Type::getVoidTy(Context),
                                    {llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt64Ty(Context),
                                     llvm::PointerType::getUnqual(Context),
                                     llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt32Ty(Context)},
                                    false));
        Builder->CreateCall(mapPutFn,
                            {mapPtr, keyHash, valRawPtr, strPtr, strLen});
        return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
      }

      if (methodName == "get") {
        llvm::FunctionCallee mapGetFn = Module->getOrInsertFunction(
            "maml_map_get",
            llvm::FunctionType::get(llvm::PointerType::getUnqual(Context),
                                    {llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt64Ty(Context),
                                     llvm::PointerType::getUnqual(Context),
                                     llvm::Type::getInt32Ty(Context)},
                                    false));
        return Builder->CreateCall(mapGetFn, {mapPtr, keyHash, strPtr, strLen});
      }
    }
  }

  // --- 2. STANDARD IDENTIFIER CALLS ---
  if (expr["function"]["node_type"] == "Identifier") {
    std::string funcName = expr["function"]["value"];

    if (funcName == "spawn") {
      llvm::Value *taskArg = evaluateExpression(expr["arguments"][0]["value"]);
      llvm::FunctionCallee spawnFn = Module->getOrInsertFunction(
          "maml_spawn_task", llvm::Type::getVoidTy(Context),
          llvm::PointerType::getUnqual(Context));
      Builder->CreateCall(spawnFn, {taskArg});
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
    }
    if (funcName == "run_executor") {
      llvm::FunctionCallee runFn = Module->getOrInsertFunction(
          "maml_run_executor", llvm::Type::getVoidTy(Context));
      Builder->CreateCall(runFn, {});
      return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
    }

    if (funcName == "puts") {
      llvm::FunctionCallee putsFn = Module->getOrInsertFunction(
          "puts", llvm::FunctionType::get(
                      llvm::Type::getInt32Ty(Context),
                      {llvm::PointerType::getUnqual(Context)}, false));
      llvm::Value *strStruct =
          evaluateExpression(expr["arguments"][0]["value"]);
      llvm::Type *strTy =
          llvmTypeFor(expr["arguments"][0]["value"]["maml_type"]);

      // Auto-promote to stack if it's not a pointer (e.g. if loaded from an
      // index)
      llvm::Value *strPtr = strStruct;
      if (!strPtr->getType()->isPointerTy()) {
        llvm::AllocaInst *tmp = Builder->CreateAlloca(strTy);
        Builder->CreateStore(strStruct, tmp);
        strPtr = tmp;
      }

      llvm::Value *strPtrGep = Builder->CreateGEP(
          strTy, strPtr, {Builder->getInt32(0), Builder->getInt32(0)});
      llvm::Value *charPtr =
          Builder->CreateLoad(llvm::PointerType::getUnqual(Context), strPtrGep);
      return Builder->CreateCall(putsFn, {charPtr});
    }

    llvm::Function *calleeFn = Module->getFunction(funcName);
    if (!calleeFn) {
      // Create external declaration if not found in module
      std::vector<llvm::Type *> argTypes;
      for (const auto &arg : expr["arguments"]) {
        argTypes.push_back(llvmTypeFor(arg["value"]["maml_type"]));
      }
      llvm::Type *retType = llvmTypeFor(expr["maml_type"]);
      llvm::FunctionType *FT =
          llvm::FunctionType::get(retType, argTypes, false);
      calleeFn = llvm::Function::Create(FT, llvm::Function::ExternalLinkage,
                                        funcName, *Module);
    }

    std::vector<llvm::Value *> argsV;
    for (const auto &arg : expr["arguments"]) {
      llvm::Value *val = evaluateExpression(arg["value"]);

      // Ownership transfer check
      if (arg.value("is_own", false)) {
        if (arg["value"]["node_type"] == "Identifier") {
          std::string varName = arg["value"]["value"];
          if (llvm::Value *ptr = resolveSymbol(varName)) {
            untrackFromRelease(ptr);
          }
        }
      }
      argsV.push_back(val);
    }
    return Builder->CreateCall(calleeFn, argsV, "calltmp");
  }

  return nullptr;
}

// =========================================================================
// PHASE 3: CONTROL FLOW & CONTAINER OPERATIONS
// =========================================================================

llvm::Value *CodegenBackend::compileIfExpr(const json &e) {
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *thenBB = llvm::BasicBlock::Create(Context, "then", F);
  llvm::BasicBlock *elseBB = llvm::BasicBlock::Create(Context, "else", F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(Context, "merge", F);

  llvm::Value *cond = evaluateExpression(e["condition"]);
  Builder->CreateCondBr(cond, thenBB, elseBB);

  // --- Consequence ---
  Builder->SetInsertPoint(thenBB);
  llvm::Value *thenVal = evaluateExpression(e["consequence"]);
  // Guard branch to avoid crashing LLVM if block terminated early (e.g.,
  // 'return')
  if (!Builder->GetInsertBlock()->getTerminator()) {
    Builder->CreateBr(mergeBB);
  }
  thenBB = Builder->GetInsertBlock(); // Update in case of nested blocks

  // --- Alternative ---
  Builder->SetInsertPoint(elseBB);
  llvm::Value *elseVal = nullptr;
  if (e.contains("alternative")) {
    elseVal = evaluateExpression(e["alternative"]);
  } else if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" &&
             e["maml_type"]["kind"] != "Unknown") {
    // Only generate a fallback null value if we actually expect an assignment
    // result
    elseVal = llvm::Constant::getNullValue(llvmTypeFor(e["maml_type"]));
  }

  if (!Builder->GetInsertBlock()->getTerminator()) {
    Builder->CreateBr(mergeBB);
  }
  elseBB = Builder->GetInsertBlock();

  // --- Merge ---
  Builder->SetInsertPoint(mergeBB);

  // Only create a PHI node if the IfExpr is returning a value and both branches
  // provided one
  if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" &&
      e["maml_type"]["kind"] != "Unknown" && thenVal && elseVal) {
    llvm::PHINode *phi =
        Builder->CreatePHI(llvmTypeFor(e["maml_type"]), 2, "iftmp");
    phi->addIncoming(thenVal, thenBB);
    phi->addIncoming(elseVal, elseBB);
    return phi;
  }

  return nullptr;
}

// =========================================================================
// LOGICAL SHORT-CIRCUIT EVALUATION
// =========================================================================

llvm::Value *CodegenBackend::compileLogicalAnd(const json &expr,
                                               llvm::Value *leftVal) {
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *rightBB = llvm::BasicBlock::Create(Context, "and_right", F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(Context, "and_merge", F);

  // If left is true, go check right. If left is false, short-circuit to merge.
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();
  Builder->CreateCondBr(leftVal, rightBB, mergeBB);

  // Right Block
  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);

  // Merge Block
  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi =
      Builder->CreatePHI(llvm::Type::getInt1Ty(Context), 2, "and_phi");

  // If we came from leftExitBB, left must have been false, so the whole AND is
  // false (0).
  phi->addIncoming(llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0),
                   leftExitBB);
  // If we came from rightExitBB, the result is whatever the right side
  // evaluated to.
  phi->addIncoming(rightVal, rightExitBB);

  return phi;
}

llvm::Value *CodegenBackend::compileLogicalOr(const json &expr,
                                              llvm::Value *leftVal) {
  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *rightBB = llvm::BasicBlock::Create(Context, "or_right", F);
  llvm::BasicBlock *mergeBB = llvm::BasicBlock::Create(Context, "or_merge", F);

  // If left is true, short-circuit to merge. If left is false, go check right.
  llvm::BasicBlock *leftExitBB = Builder->GetInsertBlock();
  Builder->CreateCondBr(leftVal, mergeBB, rightBB);

  // Right Block
  Builder->SetInsertPoint(rightBB);
  llvm::Value *rightVal = evaluateExpression(expr["right"]);
  llvm::BasicBlock *rightExitBB = Builder->GetInsertBlock();
  Builder->CreateBr(mergeBB);

  // Merge Block
  Builder->SetInsertPoint(mergeBB);
  llvm::PHINode *phi =
      Builder->CreatePHI(llvm::Type::getInt1Ty(Context), 2, "or_phi");

  // If we came from leftExitBB, left must have been true, so the whole OR is
  // true (1).
  phi->addIncoming(llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 1),
                   leftExitBB);
  // If we came from rightExitBB, the result is whatever the right side
  // evaluated to.
  phi->addIncoming(rightVal, rightExitBB);

  return phi;
}

llvm::Value *CodegenBackend::compileMatchExpr(const json &e) {
  llvm::Value *subject = evaluateExpression(e["subject"]);

  // We need the type definition to calculate the memory offsets during
  // destructuring
  json subjectType = e["subject"]["maml_type"];

  llvm::Function *F = Builder->GetInsertBlock()->getParent();
  llvm::BasicBlock *mergeBB =
      llvm::BasicBlock::Create(Context, "match_merge", F);

  std::vector<std::pair<llvm::Value *, llvm::BasicBlock *>> incomings;

  for (const auto &arm : e["arms"]) {
    llvm::BasicBlock *armBB = llvm::BasicBlock::Create(Context, "match_arm", F);
    llvm::BasicBlock *nextBB =
        llvm::BasicBlock::Create(Context, "match_next", F);

    llvm::Value *isMatch = compilePatternCheck(arm["pattern"], subject);
    Builder->CreateCondBr(isMatch, armBB, nextBB);

    // --- Compile Arm Body ---
    Builder->SetInsertPoint(armBB);

    // 1. Snapshot the keys we are about to inject so we can clean them up later
    std::vector<std::string> injectedKeys;
    if (arm["pattern"]["kind"] == "Variant") {
      if (arm["pattern"].contains("binding")) {
        injectedKeys.push_back(arm["pattern"]["binding"]);
      }
      if (arm["pattern"].contains("fields")) {
        for (const auto &fb : arm["pattern"]["fields"]) {
          injectedKeys.push_back(fb["binding"]);
        }
      }
      // Extract the payload and push into the SymbolTable
      injectPatternBindings(arm["pattern"], subject, subjectType);
    }

    // 2. Evaluate the body with the destructured variables active
    llvm::Value *val = evaluateExpression(arm["body"]);

    // 3. Cleanup injected variables so they don't leak into the next arm or
    // outer scope
    for (const auto &key : injectedKeys) {
      SymbolTable.erase(key);
    }

    Builder->CreateBr(mergeBB);
    incomings.push_back({val, Builder->GetInsertBlock()});

    // Move to next arm
    Builder->SetInsertPoint(nextBB);
  }

  // After all arms, jump to merge
  Builder->CreateBr(mergeBB);
  Builder->SetInsertPoint(mergeBB);

  // If the match yields a value, build the Phi node
  if (e.contains("maml_type") && e["maml_type"]["kind"] != "Unit" &&
      e["maml_type"]["kind"] != "Unknown") {
    llvm::PHINode *phi =
        Builder->CreatePHI(llvmTypeFor(e["maml_type"]), incomings.size());
    for (auto &pair : incomings) {
      if (pair.first) {
        phi->addIncoming(pair.first, pair.second);
      }
    }
    return phi;
  }

  return llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0);
}

llvm::Value *CodegenBackend::compilePatternCheck(const json &pattern,
                                                 llvm::Value *subject) {
  std::string kind = pattern["kind"];

  // 1. Wildcard: Always matches
  if (kind == "Wildcard") {
    return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 1);
  }

  // 2. Literal: Compare subject value to the pattern value
  if (kind == "Literal") {
    llvm::Type *valType = llvmTypeFor(pattern["value"]["maml_type"]);
    llvm::Value *subjectVal =
        Builder->CreateLoad(valType, subject, "match.lit.load");
    llvm::Value *litVal = evaluateExpression(pattern["value"]);
    return Builder->CreateICmpEQ(subjectVal, litVal, "match.lit.cmp");
  }

  // 3. Variant: Load discriminant and compare
  if (kind == "Variant") {
    // Layout is { i32 discriminant, [MaxPayload x i8] payload } [cite: 64, 102]
    // GEP to Field 0 (discriminant)
    llvm::Value *discrimPtr = Builder->CreateGEP(
        nullptr, subject,
        {llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0),
         llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0)},
        "match.discrim.ptr");

    llvm::Value *actualDiscrim = Builder->CreateLoad(
        llvm::Type::getInt32Ty(Context), discrimPtr, "match.discrim.val");

    // Expected discriminant (passed from sema via JSON) [cite: 103, 104]
    int expected = pattern["discriminant"];
    llvm::Value *expectedDiscrim =
        llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), expected);

    return Builder->CreateICmpEQ(actualDiscrim, expectedDiscrim,
                                 "match.discrim.cmp");
  }

  return llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);
}

// =========================================================================
// PHASE 4: PATTERN DESTRUCTURING & BINDING
// =========================================================================

void CodegenBackend::injectPatternBindings(const json &pattern,
                                           llvm::Value *subject,
                                           const json &subjectType) {
  if (pattern["kind"] != "Variant")
    return;

  std::string variantName = pattern["name"];

  // 1. Find the variant definition in the SumType JSON
  json variantDef;
  for (const auto &v : subjectType["variants"]) {
    if (v["name"] == variantName) {
      variantDef = v;
      break;
    }
  }

  // If it's a unit variant (no fields), there is nothing to extract
  if (variantDef["fields"].empty())
    return;

  llvm::Type *sumTy = llvmTypeFor(subjectType);

  // 2. GEP into the payload slot (Index 1 of the { i32, [MaxPayload x i8] }
  // struct)
  llvm::Value *payloadPtr = Builder->CreateGEP(
      sumTy, subject, {Builder->getInt32(0), Builder->getInt32(1)},
      "payload_raw_ptr");

  // 3. Reconstruct the typed payload struct
  // We must do this so LLVM knows the exact byte offsets of the fields inside
  // the payload array
  std::vector<llvm::Type *> payloadTypes;
  for (const auto &f : variantDef["fields"]) {
    payloadTypes.push_back(llvmTypeFor(f["type"]));
  }
  llvm::StructType *payloadStructTy =
      llvm::StructType::get(Context, payloadTypes);

  // 4. Bitcast the raw byte array pointer to our typed struct pointer
  llvm::Value *typedPayloadPtr = Builder->CreatePointerCast(
      payloadPtr, llvm::PointerType::getUnqual(Context), "payload_typed_ptr");

  // 5. Handle Single Binding: case Circle(c) => body
  if (pattern.contains("binding")) {
    std::string bindName = pattern["binding"];
    // The entire payload is treated as a single value. We bind the pointer to
    // the first internal field.
    llvm::Value *fieldPtr = Builder->CreateGEP(
        payloadStructTy, typedPayloadPtr,
        {Builder->getInt32(0), Builder->getInt32(0)}, "single_bind_ptr");
    SymbolTable[bindName] = fieldPtr;
  }

  // 6. Handle Field Bindings: case Circle{radius: r} => body
  if (pattern.contains("fields")) {
    for (const auto &fb : pattern["fields"]) {
      std::string fieldName = fb["field"];
      std::string bindName = fb["binding"];

      // Find the structural index of the requested field
      int index = -1;
      for (int i = 0; i < variantDef["fields"].size(); ++i) {
        if (variantDef["fields"][i]["name"] == fieldName) {
          index = i;
          break;
        }
      }

      if (index >= 0) {
        llvm::Value *fieldPtr = Builder->CreateGEP(
            payloadStructTy, typedPayloadPtr,
            {Builder->getInt32(0), Builder->getInt32(index)}, "field_bind_ptr");
        // Inject into the environment!
        SymbolTable[bindName] = fieldPtr;
      }
    }
  }
}

llvm::Value *CodegenBackend::compileFieldAccess(const json &e) {
  llvm::Value *objVal = evaluateExpression(e["object"]);
  std::string fieldName = e["field"];

  // 1. Resolve Struct Type
  json structType = e["object"]["maml_type"];

  // 2. Find field index
  int index = -1;
  for (int i = 0; i < structType["fields"].size(); ++i) {
    if (structType["fields"][i]["name"] == fieldName) {
      index = i;
      break;
    }
  }

  // 3. Resolve the actual LLVM type for the struct
  llvm::Type *baseTy = llvmTypeFor(structType);

  // --- FIX: Spill to stack if we were handed a loaded value instead of a
  // pointer ---
  llvm::Value *objPtr = objVal;
  if (!objVal->getType()->isPointerTy()) {
    llvm::AllocaInst *spill =
        Builder->CreateAlloca(baseTy, nullptr, "struct_spill");
    Builder->CreateStore(objVal, spill);
    objPtr = spill;
  }

  // 4. Create GEP using the guaranteed pointer
  return Builder->CreateGEP(baseTy, objPtr,
                            {Builder->getInt32(0), Builder->getInt32(index)},
                            "fieldptr");
}

llvm::Value *CodegenBackend::compileArrayLiteral(const json &e) {
  // 1. Get the LLVM type for the array (e.g., [N x i32])
  llvm::Type *arrayType = llvmTypeFor(e["maml_type"]);

  bool isHeap = e.value("is_heap", false);
  llvm::Value *arrayPtr = nullptr;

  if (isHeap) {
    // Dynamically allocate on the heap
    llvm::DataLayout DL(Module.get());
    uint64_t allocSize = DL.getTypeAllocSize(arrayType);

    // --- FIX: Change getInt32Ty to getInt64Ty to match Zig's usize ---
    llvm::FunctionCallee mamlAlloc = Module->getOrInsertFunction(
        "maml_alloc",
        llvm::FunctionType::get(llvm::PointerType::getUnqual(Context),
                                {llvm::Type::getInt64Ty(Context)}, false));

    // --- FIX: Pass the allocSize as a 64-bit constant ---
    arrayPtr = Builder->CreateCall(
        mamlAlloc,
        {llvm::ConstantInt::get(llvm::Type::getInt64Ty(Context), allocSize)},
        "array_heap_alloc");
  } else {
    // Safely allocate on the stack
    arrayPtr = Builder->CreateAlloca(arrayType, nullptr, "array_stack_alloc");
  }

  // 3. Iterate through elements and store them
  int i = 0;
  if (e.contains("elements") && e["elements"].is_array()) {
    for (const auto &element : e["elements"]) {
      llvm::Value *val = evaluateExpression(element);

      // Calculate index: array[i]
      llvm::Value *index = Builder->CreateGEP(
          arrayType, arrayPtr,
          {llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), 0),
           llvm::ConstantInt::get(llvm::Type::getInt32Ty(Context), i++)},
          "elemptr");

      Builder->CreateStore(val, index);
    }
  }

  // 4. ARC Tracking
  trackForRelease(arrayPtr, isHeap);
  return arrayPtr;
}

void CodegenBackend::compileForLoop(const json &s) {
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  llvm::BasicBlock *condBB =
      llvm::BasicBlock::Create(Context, "for.cond", parentFn);
  llvm::BasicBlock *bodyBB =
      llvm::BasicBlock::Create(Context, "for.body", parentFn);
  llvm::BasicBlock *loopExitBB =
      llvm::BasicBlock::Create(Context, "for.exit", parentFn);

  if (s.contains("init") && !s["init"].is_null()) {
    compileStatement(s["init"]);
  }
  Builder->CreateBr(condBB);

  Builder->SetInsertPoint(condBB);
  if (s.contains("condition") && !s["condition"].is_null()) {
    llvm::Value *condVal = evaluateExpression(s["condition"]);
    Builder->CreateCondBr(condVal, bodyBB, loopExitBB);
  } else {
    Builder->CreateBr(bodyBB);
  }

  Builder->SetInsertPoint(bodyBB);
  if (s.contains("body") && !s["body"].is_null()) {
    compileStatement(s["body"]);
  }

  if (s.contains("post") && !s["post"].is_null()) {
    compileStatement(s["post"]);
  }
  Builder->CreateBr(condBB);

  Builder->SetInsertPoint(loopExitBB);
}

llvm::Value *CodegenBackend::compileAwaitExpression(const json &e) {
  llvm::Value *taskVal = evaluateExpression(e["value"]);
  llvm::Function *parentFn = Builder->GetInsertBlock()->getParent();

  llvm::BasicBlock *resumeBB =
      llvm::BasicBlock::Create(Context, "await.resume", parentFn);
  llvm::BasicBlock *cleanupBB =
      llvm::BasicBlock::Create(Context, "await.cleanup", parentFn);
  llvm::BasicBlock *suspendBB =
      llvm::BasicBlock::Create(Context, "await.suspend", parentFn);

  llvm::Function *coroSuspendFn = llvm::Intrinsic::getDeclaration(
      Module.get(), llvm::Intrinsic::coro_suspend);

  llvm::Value *noneToken = llvm::ConstantTokenNone::get(Context);
  llvm::Value *isFinalValue =
      llvm::ConstantInt::get(llvm::Type::getInt1Ty(Context), 0);

  llvm::Value *suspendResult = Builder->CreateCall(
      coroSuspendFn, {noneToken, isFinalValue}, "suspend_res");

  llvm::SwitchInst *sw = Builder->CreateSwitch(suspendResult, suspendBB, 2);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 0),
              resumeBB);
  sw->addCase(llvm::ConstantInt::get(llvm::Type::getInt8Ty(Context), 1),
              cleanupBB);

  Builder->SetInsertPoint(cleanupBB);
  llvm::Function *coroFreeFn =
      llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_free);
  llvm::Value *freeMem = Builder->CreateCall(
      coroFreeFn, {resolveSymbol("__coro_id"), resolveSymbol("__coro_hdl")});
  Builder->CreateBr(suspendBB);

  Builder->SetInsertPoint(suspendBB);
  Builder->CreateRet(resolveSymbol("__coro_hdl"));

  Builder->SetInsertPoint(resumeBB);
  return taskVal;
}

// =========================================================================
// PHASE 5: AUTOMATIC REFERENCE COUNTING (ARC) PARITY
// =========================================================================

bool CodegenBackend::isHeapManagedType(const json &typeJson) {
  if (typeJson.is_null())
    return false;
  std::string kind = typeJson["kind"];
  // MAML Reference Types that utilize the mi_malloc heap in Zig
  return kind == "String" || kind == "Vector" || kind == "Map" ||
         kind == "Task" || kind == "Slice";
}

bool CodegenBackend::isRefType(const json &typeJson) {
  if (typeJson.is_null())
    return false;
  std::string kind = typeJson["kind"];
  // Only these types are allocated via maml_alloc and have ArcHeaders
  return kind == "String" || kind == "Vector" || kind == "Map" ||
         kind == "Task" || kind == "Slice";
}

bool CodegenBackend::needsARC(const json &typeJson) {
  if (isRefType(typeJson))
    return true;
  if (typeJson["kind"] == "Struct") {
    for (const auto &field : typeJson["fields"]) {
      if (needsARC(field["type"]))
        return true;
    }
  }
  return false;
}

llvm::Value *CodegenBackend::extractHeapPointer(llvm::Value *containerPtr,
                                                const json &typeJson) {
  // Complex container headers consistently place the dynamic heap pointer at
  // struct index 0
  llvm::Type *ty = llvmTypeFor(typeJson);
  llvm::Value *rawPtrGep = Builder->CreateGEP(
      ty, containerPtr, {Builder->getInt32(0), Builder->getInt32(0)},
      "arc_raw_gep");
  return Builder->CreateLoad(llvm::PointerType::getUnqual(Context), rawPtrGep,
                             "arc_raw_load");
}

void CodegenBackend::emitRetain(llvm::Value *valPtr, const json &typeJson) {
  if (!isHeapManagedType(typeJson))
    return;
  llvm::Value *rawPtr = extractHeapPointer(valPtr, typeJson);

  llvm::FunctionCallee retainFn =
      Module->getOrInsertFunction("maml_retain", llvm::Type::getVoidTy(Context),
                                  llvm::PointerType::getUnqual(Context));
  Builder->CreateCall(retainFn, {rawPtr});
}

void CodegenBackend::emitRelease(llvm::Value *valPtr, const json &typeJson) {
  if (!isHeapManagedType(typeJson))
    return;
  llvm::Value *rawPtr = extractHeapPointer(valPtr, typeJson);

  llvm::FunctionCallee releaseFn = Module->getOrInsertFunction(
      "maml_release", llvm::Type::getVoidTy(Context),
      llvm::PointerType::getUnqual(Context));
  Builder->CreateCall(releaseFn, {rawPtr});
}

void CodegenBackend::trackDeepForRelease(llvm::Value *valPtr,
                                         const json &typeJson) {
  // 1. If it's a RefType (Vector, etc.), it MUST be tracked because it has a
  // header.
  if (isRefType(typeJson)) {
    ScopeStack.back().push_back({valPtr, false, typeJson});
    return;
  }

  // 2. If it's a Struct, we do NOT track the struct itself.
  // We ONLY recurse to find fields that might be RefTypes.
  if (typeJson["kind"] == "Struct") {
    llvm::Type *structTy = llvmTypeFor(typeJson);
    for (int i = 0; i < typeJson["fields"].size(); ++i) {
      const auto &field = typeJson["fields"][i];
      // Only track if the nested field is something that needs an ArcHeader
      if (isRefType(field["type"]) || field["type"]["kind"] == "Struct") {
        llvm::Value *fieldPtr = Builder->CreateGEP(
            structTy, valPtr, {Builder->getInt32(0), Builder->getInt32(i)});
        trackDeepForRelease(fieldPtr, field["type"]);
      }
    }
  }
  // 3. Ignore Primitives (int, bool) and POD Structs.
}

void CodegenBackend::untrackFromRelease(llvm::Value *valPtr) {
  if (ScopeStack.empty())
    return;
  auto &currentScope = ScopeStack.back();
  auto it = std::remove_if(
      currentScope.begin(), currentScope.end(),
      [valPtr](const TrackedItem &item) { return item.ptr == valPtr; });
  if (it != currentScope.end()) {
    currentScope.erase(it, currentScope.end());
  }
}

void CodegenBackend::compileProgram(const json &ast) {
  if (ast["node_type"] != "Program")
    return;

  llvm::Type *voidTy = llvm::Type::getVoidTy(Context);
  llvm::Type *ptrTy = llvm::PointerType::getUnqual(Context);
  llvm::Type *boolTy = llvm::Type::getInt1Ty(Context);

  // 1. Declare the LLVM Coroutine Intrinsics
  llvm::Function *coroResumeIntrin = llvm::Intrinsic::getDeclaration(
      Module.get(), llvm::Intrinsic::coro_resume);
  llvm::Function *coroDestroyIntrin = llvm::Intrinsic::getDeclaration(
      Module.get(), llvm::Intrinsic::coro_destroy);
  llvm::Function *coroDoneIntrin =
      llvm::Intrinsic::getDeclaration(Module.get(), llvm::Intrinsic::coro_done);

  // 2. Generate maml_coro_resume_helper
  llvm::Function *resumeHelper = llvm::Function::Create(
      llvm::FunctionType::get(voidTy, {ptrTy}, false),
      llvm::Function::ExternalLinkage, "maml_coro_resume_helper", *Module);
  llvm::BasicBlock *resumeBB =
      llvm::BasicBlock::Create(Context, "entry", resumeHelper);
  Builder->SetInsertPoint(resumeBB);
  Builder->CreateCall(coroResumeIntrin, {resumeHelper->getArg(0)});
  Builder->CreateRetVoid();

  // 3. Generate maml_coro_destroy_helper
  llvm::Function *destroyHelper = llvm::Function::Create(
      llvm::FunctionType::get(voidTy, {ptrTy}, false),
      llvm::Function::ExternalLinkage, "maml_coro_destroy_helper", *Module);
  llvm::BasicBlock *destroyBB =
      llvm::BasicBlock::Create(Context, "entry", destroyHelper);
  Builder->SetInsertPoint(destroyBB);
  Builder->CreateCall(coroDestroyIntrin, {destroyHelper->getArg(0)});
  Builder->CreateRetVoid();

  // 4. Generate maml_coro_done_helper
  llvm::Function *doneHelper = llvm::Function::Create(
      llvm::FunctionType::get(boolTy, {ptrTy}, false),
      llvm::Function::ExternalLinkage, "maml_coro_done_helper", *Module);
  llvm::BasicBlock *doneBB =
      llvm::BasicBlock::Create(Context, "entry", doneHelper);
  Builder->SetInsertPoint(doneBB);
  llvm::Value *doneResult =
      Builder->CreateCall(coroDoneIntrin, {doneHelper->getArg(0)});
  Builder->CreateRet(doneResult);

  // 5. Proceed to compile the user's AST
  for (const auto &decl : ast["decls"]) {
    if (decl["node_type"] == "FnDecl") {
      compileFunction(decl);
    }
  }
}

int main(int argc, char *argv[]) {
  if (argc < 2) {
    std::cerr << "Usage: maml_cc <ast_json_file>\n";
    return 1;
  }
  std::ifstream f(argv[1]);
  if (!f.is_open()) {
    std::cerr << "Could not open target input JSON file.\n";
    return 1;
  }

  json ast = json::parse(f);

  CodegenBackend backend("maml_core_module");
  backend.compileProgram(ast);

  if (llvm::verifyModule(*backend.Module, &llvm::errs())) {
    std::cerr << "\nBackend Error: Internal generated LLVM IR module contains "
                 "structural verification errors!\n";
    return 1;
  }

  backend.Module->print(llvm::outs(), nullptr);
  return 0;
}